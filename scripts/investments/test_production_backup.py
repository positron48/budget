"""Operator acceptance: real PostgreSQL/dump/restore via a synthetic kube transport.

Run with BUDGET_OPERATOR_INTEGRATION=1 and BUDGET_BACKUP_BINARY=/path/to/budgetbackup.
The transport is not a Kubernetes acceptance test; database and native tools are real.
"""
import hashlib
import json
import os
import re
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
import uuid

from production_backup import Failure, flux_revision

ROOT = Path(__file__).resolve().parents[2]


class FluxGenerationTests(unittest.TestCase):
    def flux(self, aggregate=None, ready_generation=None):
        ready = {"type": "Ready", "status": "True"}
        if ready_generation is not None:
            ready["observedGeneration"] = ready_generation
        status = {"conditions": [ready], "lastAppliedRevision": "main@sha1:fixture"}
        if aggregate is not None:
            status["observedGeneration"] = aggregate
        return {"metadata": {"generation": 3}, "status": status}

    def test_current_ready_condition_is_authoritative_when_aggregate_missing_or_sentinel(self):
        for aggregate in (None, -1, 2):
            with self.subTest(aggregate=aggregate):
                self.assertEqual("main@sha1:fixture", flux_revision(self.flux(aggregate, 3)))

    def test_legacy_aggregate_generation_is_accepted_when_ready_generation_omitted(self):
        self.assertEqual("main@sha1:fixture", flux_revision(self.flux(3)))

    def test_stale_ready_cannot_borrow_current_aggregate_generation(self):
        with self.assertRaises(Failure):
            flux_revision(self.flux(3, 2))

    def test_absent_or_stale_generation_is_not_assumed_current(self):
        for aggregate in (None, -1, 2):
            with self.subTest(aggregate=aggregate), self.assertRaises(Failure):
                flux_revision(self.flux(aggregate))

    def test_notready_reconciling_stalled_and_missing_revision_are_rejected(self):
        for state in ("notready", "Reconciling", "Stalled", "missing_revision"):
            flux = self.flux(3, 3)
            if state == "notready":
                flux["status"]["conditions"][0]["status"] = "False"
            elif state == "missing_revision":
                del flux["status"]["lastAppliedRevision"]
            else:
                flux["status"]["conditions"].append({"type": state, "status": "True"})
            with self.subTest(state=state), self.assertRaises(Failure):
                flux_revision(flux)

TRANSPORT = r'''#!/usr/bin/env python3
import base64,json,os,select,socket,socketserver,subprocess,sys
from pathlib import Path
args=sys.argv[1:]
container=os.environ['BUDGET_OPERATOR_TEST_CONTAINER']
if 'exec' in args:
 assert args[args.index('-c')+1]=='postgres'
 cmd=args[args.index('--')+1:]
 if Path(os.environ['BUDGET_OPERATOR_TEST_BLOCK']).exists() and '--format=custom' in cmd:
  import time
  Path(os.environ['BUDGET_OPERATOR_TEST_BLOCK']+'.started').touch()
  time.sleep(60)
 if os.environ.get('BUDGET_OPERATOR_TEST_RESTORE_FAILURE') and 'pg_restore' in cmd and '--version' not in cmd:
  print('operator-fixture-password: simulated unsafe stderr',file=sys.stderr)
  raise SystemExit(1)
 raise SystemExit(subprocess.call(['docker','exec','-i',container,*cmd]))
if 'port-forward' in args:
 Path(os.environ['BUDGET_OPERATOR_TEST_FORWARD_PID']).write_text(str(os.getpid()))
 local=int(args[-1].split(':')[0])
 remote=int(os.environ['BUDGET_OPERATOR_TEST_PORT'])
 class Proxy(socketserver.BaseRequestHandler):
  def handle(self):
   with socket.create_connection(('127.0.0.1',remote)) as peer:
    while True:
     ready,_,_=select.select([self.request,peer],[],[],1)
     for source in ready:
      data=source.recv(65536)
      if not data: return
      (peer if source is self.request else self.request).sendall(data)
 class Server(socketserver.ThreadingTCPServer):
  allow_reuse_address=True
  daemon_threads=True
 with Server(('127.0.0.1',local),Proxy) as server: server.serve_forever()
 raise SystemExit(0)
fixture=json.loads(Path(os.environ['BUDGET_OPERATOR_TEST_FIXTURE']).read_text())
i=args.index('get')
kind=args[i+1]
def declared(name):
 return {'name':'postgres' if name=='budget-postgres' else name,'image':'fixture/'+name+':current','env':[
 {'name':'POSTGRES_USER','value':'budget'},{'name':'POSTGRES_DB','value':'budget'},
 {'name':'POSTGRES_PASSWORD','valueFrom':{'secretKeyRef':{'name':'pg-secret','key':'password'}}}]}
if kind=='deployment':
 name=args[i+2]
 result={'metadata':{'generation':1},'spec':{'replicas':1,'selector':{'matchLabels':{'app':name}},'template':{'spec':{'containers':[declared(name)]}}},
 'status':{'observedGeneration':1,'updatedReplicas':1,'availableReplicas':1,'readyReplicas':0 if fixture.get('unready') else 1}}
elif kind=='pods':
 name=args[args.index('-l')+1].split('=')[1]
 current=declared(name)
 if fixture.get('old_pod'): current['image']='fixture/'+name+':old'
 result={'items':[{'metadata':{'name':name+'-fixture','uid':name+'-uid'},'spec':{'containers':[current]},'status':{'phase':'Running','conditions':[{'type':'Ready','status':'True'}],
 'containerStatuses':[{'name':current['name'],'ready':True,'imageID':('docker.io/library/postgres@sha256:' if name=='budget-postgres' else 'ghcr.io/fixture/'+name+'@sha256:')+'1'*64}]}}]}
elif kind=='secret': result={'data':{'password':base64.b64encode(b'operator-fixture-password').decode()}}
else: result={'metadata':{'generation':1},'status':{'observedGeneration':1,'conditions':[{'type':'Ready','status':'True'}],'lastAppliedRevision':'main@sha1:'+'2'*40}}
print(json.dumps(result))
'''


@unittest.skipUnless(os.environ.get("BUDGET_OPERATOR_INTEGRATION") == "1", "set BUDGET_OPERATOR_INTEGRATION=1")
class ProductionBackupAcceptance(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix="budget-operator-test-")
        cls.root = Path(cls.temp.name)
        cls.binary = Path(os.environ["BUDGET_BACKUP_BINARY"]).resolve()
        cls.sha = hashlib.sha256(cls.binary.read_bytes()).hexdigest()
        cls.container = "budget-operator-test-" + uuid.uuid4().hex
        try:
            cls.docker("run", "-d", "--name", cls.container, "-p", "127.0.0.1::5432",
                       "-e", "POSTGRES_USER=budget", "-e", "POSTGRES_DB=budget",
                       "-e", "POSTGRES_PASSWORD=operator-fixture-password", "postgres:15-alpine")
            for _ in range(100):
                if cls.docker("exec", cls.container, "pg_isready", "-h", "127.0.0.1", "-U", "budget", check=False).returncode == 0:
                    break
                time.sleep(0.1)
            else:
                raise RuntimeError("disposable PostgreSQL did not become ready")
            for path in sorted((ROOT / "migrations").glob("*.up.sql")):
                if int(path.name.split("_")[0]) <= 11:
                    cls.sql(path.read_text())
            cls.sql("""CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL);
INSERT INTO schema_migrations VALUES(11,false);
INSERT INTO tenants(name,default_currency_code) VALUES('Operator fixture tenant','RUB');
INSERT INTO users(email,password_hash,name) VALUES('operator@example.test','private-fixture-hash','private-fixture-user');
INSERT INTO user_tenants(user_id,tenant_id,role) SELECT u.id,t.id,'owner' FROM users u CROSS JOIN tenants t;
INSERT INTO asset_accounts(tenant_id,name,kind,fixed_currency_code) SELECT id,'Legacy property','property','RUB' FROM tenants;
INSERT INTO asset_snapshots(tenant_id,account_id,currency_code,amount_minor,as_of,kind) SELECT tenant_id,id,'RUB',9007199254740993,'2026-10-01','valuation' FROM asset_accounts;
""")
            port = cls.docker("port", cls.container, "5432").stdout.strip().rsplit(":", 1)[1]
            cls.fixture = cls.root / "fixture.json"
            cls.fixture.write_text("{}")
            cls.transport = cls.root / "kubectl-fixture"
            # NativeTools intentionally gives child processes a minimal env.
            # Bind this synthetic transport to its own disposable resources,
            # just as k3s resolves its fixed on-server config without test env.
            transport = TRANSPORT.replace("os.environ['BUDGET_OPERATOR_TEST_CONTAINER']", repr(cls.container))
            transport = transport.replace("os.environ['BUDGET_OPERATOR_TEST_PORT']", repr(port))
            transport = transport.replace("os.environ['BUDGET_OPERATOR_TEST_FIXTURE']", repr(str(cls.fixture)))
            transport = transport.replace("os.environ['BUDGET_OPERATOR_TEST_BLOCK']", repr(str(cls.root / 'block-dump')))
            transport = transport.replace("os.environ['BUDGET_OPERATOR_TEST_FORWARD_PID']", repr(str(cls.root / 'forward-pid')))
            transport = transport.replace("os.environ.get('BUDGET_OPERATOR_TEST_RESTORE_FAILURE')", "Path(" + repr(str(cls.root / 'fail-restore')) + ").exists()")
            cls.transport.write_text(transport)
            cls.transport.chmod(0o700)
            cls.env = {**os.environ, "BUDGET_OPERATOR_TEST_CONTAINER": cls.container,
                       "BUDGET_OPERATOR_TEST_PORT": port, "BUDGET_OPERATOR_TEST_FIXTURE": str(cls.fixture),
                       # These inherited credentials must never be used.
                       "DATABASE_URL": "postgres://unrelated-invalid-source",
                       "TARGET_ADMIN_DATABASE_URL": "postgres://unrelated-invalid-target"}
        except BaseException:
            cls.docker("rm", "-f", "-v", cls.container, check=False)
            cls.temp.cleanup()
            raise

    @classmethod
    def tearDownClass(cls):
        cls.docker("rm", "-f", "-v", cls.container, check=False)
        cls.temp.cleanup()

    @classmethod
    def docker(cls, *args, **kwargs):
        return subprocess.run(["docker", *args], text=True, capture_output=True,
                              check=kwargs.pop("check", True), timeout=60, **kwargs)

    @classmethod
    def sql(cls, sql, database="budget"):
        return cls.docker("exec", "-i", cls.container, "psql", "-X", "-U", "budget", "-d", database,
                          "-At", "-v", "ON_ERROR_STOP=1", input=sql).stdout.strip()

    def setUp(self):
        self.fixture.write_text("{}")
        (self.root / "fail-restore").unlink(missing_ok=True)
        (self.root / "block-dump").unlink(missing_ok=True)
        (self.root / "block-dump.started").unlink(missing_ok=True)
        (self.root / "forward-pid").unlink(missing_ok=True)
        self.sql("UPDATE schema_migrations SET dirty=false")

    def operator(self, label, *args, env=None):
        out = self.root / label
        result = subprocess.run([sys.executable, str(ROOT / "scripts/investments/production_backup.py"),
                                 "--budgetbackup", str(self.binary), "--binary-sha256", self.sha,
                                 "--kubectl", str(self.transport), "--out", str(out), *args],
                                env=env or self.env, capture_output=True, text=True, timeout=120)
        for secret in ("operator-fixture-password", "private-fixture-hash", "private-fixture-user"):
            self.assertNotIn(secret, result.stdout + result.stderr)
        return out, result

    def test_real_snapshot_backup_restore_and_source_preservation(self):
        before = self.sql("SELECT to_jsonb(u)::text FROM users u")
        out, result = self.operator("success")
        self.assertEqual(0, result.returncode, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual("backup_and_restore_verified", report["Status"])
        self.assertEqual(11, report["SchemaVersion"])
        self.assertEqual(20, report["VerifiedTables"])
        self.assertEqual(6, report["VerifiedRows"])
        self.assertEqual(before, self.sql("SELECT to_jsonb(u)::text FROM users u"))
        self.assertEqual("9007199254740993", self.sql("SELECT amount_minor FROM asset_snapshots", report["RestoreDatabase"]))
        self.assertEqual("11|f", self.sql("SELECT version,dirty FROM schema_migrations", report["RestoreDatabase"]))
        self.assertEqual(0o700, out.stat().st_mode & 0o777)
        for name in ("operator-report.json", "restore-intent.json", "restore-report.json", "backup/manifest.json", "backup/database.dump"):
            self.assertEqual(0o600, (out / name).stat().st_mode & 0o777)
        # The completed certificate is safe to share, unlike the backup itself.
        self.assertNotIn("private-fixture", (out / "operator-report.json").read_text())
        again = subprocess.run([sys.executable, str(ROOT / "scripts/investments/production_backup.py"),
                                "--budgetbackup", str(self.binary), "--binary-sha256", self.sha,
                                "--kubectl", str(self.transport), "--out", str(out)],
                               env=self.env, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(0, again.returncode)

    def test_existing_verified_backup_can_be_restored_without_new_source_dump(self):
        original, result = self.operator("reuse-original")
        self.assertEqual(0, result.returncode, result.stderr)
        manifest = original / "backup/manifest.json"
        sha = hashlib.sha256(manifest.read_bytes()).hexdigest()
        # If reuse tried to dump source, it would block on this sentinel.
        (self.root / "block-dump").touch()
        out, result = self.operator("reused", "--existing-backup", str(manifest.parent), "--manifest-sha256", sha)
        self.assertEqual(0, result.returncode, result.stderr)
        report = json.loads(result.stdout)
        self.assertTrue(report["ReusedBackup"])
        self.assertEqual(sha, report["ManifestSHA256"])
        self.assertFalse((out / "backup").exists())
        self.assertFalse((self.root / "block-dump.started").exists())

    def test_legacy_public_schema_owner_can_be_restored(self):
        self.sql("ALTER SCHEMA public OWNER TO budget")
        try:
            out, result = self.operator("legacy-public")
            self.assertEqual(0, result.returncode, result.stderr)
            # Reproduce package 157's manifest representation from a real
            # schema dump. The dump and every row fingerprint stay unchanged.
            raw = self.docker("exec", self.container, "pg_dump", "-U", "budget", "-d", "budget",
                              "--schema-only", "--no-owner", "--no-privileges").stdout
            lines = [re.sub(r"^(-- Name: .*; Type: .*; Schema: .*; Owner:) .*$",
                            r"\1 <restore-owner>", line) for line in raw.split("\n")
                     if not line.startswith(("-- Dumped from database version ", "-- Dumped by pg_dump version ",
                                             "\\restrict ", "\\unrestrict "))]
            manifest = out / "backup/manifest.json"
            value = json.loads(manifest.read_text())
            value["State"]["SchemaSHA256"] = hashlib.sha256("\n".join(lines).strip().encode()).hexdigest()
            manifest.write_text(json.dumps(value, indent=2) + "\n")
            sha = hashlib.sha256(manifest.read_bytes()).hexdigest()
            _, result = self.operator("legacy-157-manifest", "--existing-backup", str(manifest.parent),
                                      "--manifest-sha256", sha)
            self.assertEqual(0, result.returncode, result.stderr)
        finally:
            self.sql("ALTER SCHEMA public OWNER TO pg_database_owner")

    def test_real_upgrade_preserves_source_legacy_rows_and_repeats_startup(self):
        original, result = self.operator("upgrade-original")
        self.assertEqual(0, result.returncode, result.stderr)
        manifest = original / "backup/manifest.json"
        sha = hashlib.sha256(manifest.read_bytes()).hexdigest()
        before = self.sql("SELECT to_jsonb(u)::text FROM users u")
        out, result = self.operator("upgrade", "--existing-backup", str(manifest.parent),
                                    "--manifest-sha256", sha, "--rehearse-upgrade")
        self.assertEqual(0, result.returncode, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual("isolated_upgrade_verified", report["Status"])
        self.assertEqual(11, report["SourceSchemaVersion"])
        self.assertEqual(59, report["SchemaVersion"])
        self.assertEqual(19, report["PreservedTables"])
        self.assertEqual(5, report["PreservedRows"])
        self.assertTrue(report["RestartVerified"])
        self.assertEqual("11|f", self.sql("SELECT version,dirty FROM schema_migrations"))
        self.assertEqual(before, self.sql("SELECT to_jsonb(u)::text FROM users u"))
        self.assertEqual("59|f", self.sql("SELECT version,dirty FROM schema_migrations", report["RestoreDatabase"]))
        self.assertEqual("9007199254740993", self.sql("SELECT amount_minor FROM asset_snapshots", report["RestoreDatabase"]))
        self.assertEqual(0o600, (out / "restore-report.json").stat().st_mode & 0o777)

    def test_unready_deployment_stops_before_backup_or_restore(self):
        self.fixture.write_text('{"unready":true}')
        out, result = self.operator("unready")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse((out / "backup").exists())
        self.assertFalse((out / "restore-intent.json").exists())

    def test_ready_old_pod_is_not_certified_as_current_deployment(self):
        self.fixture.write_text('{"old_pod":true}')
        out, result = self.operator("old-pod")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse((out / "backup").exists())

    def test_wrong_binary_checksum_stops_before_inventory(self):
        out, result = self.operator("wrong-binary", "--binary-sha256", "0" * 64)
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(out.exists())

    def test_dirty_source_never_gets_completion_certificate(self):
        self.sql("UPDATE schema_migrations SET dirty=true")
        out, result = self.operator("dirty")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse((out / "operator-report.json").exists())
        self.assertFalse((out / "restore-intent.json").exists())

    def test_schema_mismatch_retains_backup_but_does_not_restore(self):
        out, result = self.operator("schema-mismatch", "--expected-schema", "59")
        self.assertNotEqual(0, result.returncode)
        self.assertTrue((out / "backup/manifest.json").exists())
        self.assertFalse((out / "restore-intent.json").exists())

    def test_failed_restore_retains_dump_and_target_intent_but_no_success(self):
        (self.root / "fail-restore").touch()
        out, result = self.operator("restore-failure")
        self.assertNotEqual(0, result.returncode)
        self.assertTrue((out / "backup/database.dump").exists())
        self.assertTrue((out / "restore-intent.json").exists())
        self.assertFalse((out / "operator-report.json").exists())
        self.assertFalse((out / "restore-report.json").exists())

    def test_sigterm_stops_live_forward_and_never_certifies_partial_dump(self):
        (self.root / "block-dump").touch()
        out = self.root / "interrupted"
        process = subprocess.Popen([sys.executable, str(ROOT / "scripts/investments/production_backup.py"),
                                    "--budgetbackup", str(self.binary), "--binary-sha256", self.sha,
                                    "--kubectl", str(self.transport), "--out", str(out)],
                                   env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            deadline = time.monotonic() + 30
            while not (self.root / "block-dump.started").exists():
                self.assertIsNone(process.poll(), "operator exited before the live dump wait")
                self.assertLess(time.monotonic(), deadline)
                time.sleep(0.1)
            forward_pid = int((self.root / "forward-pid").read_text())
            os.kill(forward_pid, 0)  # Verify the wait is on a live owned process.
            process.send_signal(signal.SIGTERM)
            stdout, stderr = process.communicate(timeout=15)
            self.assertNotEqual(0, process.returncode)
            self.assertNotIn("operator-fixture-password", stdout + stderr)
            self.assertFalse((out / "operator-report.json").exists())
            with self.assertRaises(ProcessLookupError):
                os.kill(forward_pid, 0)
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=5)


if __name__ == "__main__":
    unittest.main()
