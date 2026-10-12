#!/usr/bin/env python3
"""Operator-run full Budget backup and verified restore through existing k3s pods.

No deployment, source migration, source write, credential file or global kubectl
setting is created. The restore database is new and retained for inspection.
Requires Python 3, k3s/kubectl access and a reviewed native budgetbackup binary.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.parse
import uuid


class Failure(Exception):
    """Only fixed, credential-free diagnostics may leave this process."""


SAFE_BACKUP_ERRORS = {
    "upgrade requires the expected clean source schema",
    "isolated upgrade migration failed; SQL diagnostics suppressed",
    "upgrade did not reach the expected clean target schema",
    "upgrade changed or removed legacy columns", "upgrade changed legacy rows",
    "repeat startup changed the upgraded database", "upgrade rehearsal cancelled",
    "cannot inventory legacy columns", "cannot verify legacy columns", "cannot verify legacy rows",
    "database connection failed", "database version unavailable", "server version unavailable",
    "database locale unavailable; PostgreSQL 15 or 16 is required",
    "a clean Budget schema_migrations row is required", "cannot begin database snapshot",
    "cannot configure database snapshot", "cannot export database snapshot",
    "cannot inventory database tables", "cannot fingerprint database table", "cannot fingerprint database rows",
    "large objects or foreign tables require an extended verifier", "cannot inventory identity sequences",
    "unsupported identity sequence policy", "cannot read identity sequence", "cannot check identity sequence",
    "invalid identity sequence", "identity sequence is exhausted", "database schema exceeds verification limit",
    "PostgreSQL tool version unavailable", "pg_dump and pg_restore must match the PostgreSQL server major version",
    "PostgreSQL tool failed; no database credentials or SQL diagnostics were emitted",
    "PostgreSQL restore conflicts with an existing public schema", "PostgreSQL tool permission denied",
    "PostgreSQL tool authentication failed", "PostgreSQL tool cannot read this archive version",
    "PostgreSQL restore encountered duplicate key values", "PostgreSQL tool rejected database locale",
    "cannot create new restore database", "unsupported database locale provider",
    "restore requires the same PostgreSQL major version",
    "restore target must not exist; existing databases are never overwritten",
    "restored schema, rows, database settings or migration version differ from the backup snapshot",
    "restore verification transaction failed", "backup manifest checksum mismatch", "database dump checksum mismatch",
    "backup directory must be private and not a symlink", "backup files must be regular private files",
    "invalid backup manifest", "not a custom PostgreSQL dump", "invalid PostgreSQL credential format",
}


def command(argv, **kwargs):
    process = None
    stage = kwargs.pop("stage", "operator command")
    safe_errors = kwargs.pop("safe_errors", False)
    try:
        timeout = kwargs.pop("timeout", 60)
        process = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   start_new_session=True, **kwargs)
        stdout, stderr = process.communicate(timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        stop(process)
        raise Failure("operator command unavailable or timed out") from None
    except BaseException:
        stop(process)
        raise
    if process.returncode:
        messages = [line for line in stderr.decode("utf-8", errors="replace").splitlines()
                    if safe_errors and line in SAFE_BACKUP_ERRORS]
        suffix = ": " + "; ".join(messages) if messages else "; raw stderr suppressed"
        raise Failure(stage + " failed" + suffix)
    return stdout


def kube_json(kube, *args):
    try:
        return json.loads(command([*kube, *args, "-o", "json"]))
    except (ValueError, UnicodeError):
        raise Failure("invalid Kubernetes JSON response") from None


def flux_revision(flux):
    status = flux.get("status", {})
    conditions = status.get("conditions", [])
    ready = [c for c in conditions if c.get("type") == "Ready"]
    if len(ready) != 1 or ready[0].get("status") != "True":
        raise Failure("Flux Ready condition is not True")
    if any(c.get("type") in ("Reconciling", "Stalled") and c.get("status") == "True" for c in conditions):
        raise Failure("Flux is Reconciling or Stalled")
    generation = flux["metadata"]["generation"]
    # Ready's own generation describes this condition. Some controller versions
    # omit or retain a sentinel in the aggregate status.observedGeneration.
    # Never manufacture a generation when both pieces of evidence are absent.
    observed = ready[0].get("observedGeneration", status.get("observedGeneration"))
    if not isinstance(observed, int) or observed < generation:
        raise Failure("Flux Ready has no current observed generation")
    revision = status.get("lastAppliedRevision", "")
    if not revision:
        raise Failure("Flux applied revision is required")
    return revision


def inventory(kube, namespace):
    result = {}
    for name in ("budget-app", "budget-web", "budget-postgres"):
        container_name = "postgres" if name == "budget-postgres" else name
        deployment = kube_json(kube, "-n", namespace, "get", "deployment", name)
        spec, status = deployment["spec"], deployment.get("status", {})
        replicas = spec.get("replicas", 1)
        if (replicas != 1 or status.get("observedGeneration", 0) < deployment["metadata"]["generation"]
                or status.get("updatedReplicas") != 1 or status.get("availableReplicas") != 1
                or status.get("readyReplicas") != 1):
            raise Failure("Budget deployments must have one ready, updated replica")
        # Match the deployment's complete selector and pin the chosen pod UID.
        selector = spec["selector"]
        if selector.get("matchExpressions"):
            raise Failure("deployment selector expressions require operator review")
        labels = selector.get("matchLabels", {})
        if not labels:
            raise Failure("empty deployment selector")
        pods = kube_json(kube, "-n", namespace, "get", "pods", "-l",
                         ",".join(k + "=" + v for k, v in sorted(labels.items())))
        candidates = [p for p in pods["items"] if not p["metadata"].get("deletionTimestamp")
                      and p.get("status", {}).get("phase") == "Running"
                      and any(c["type"] == "Ready" and c["status"] == "True"
                              for c in p["status"].get("conditions", []))]
        if len(candidates) != 1:
            raise Failure("expected one ready pod for each Budget deployment")
        pod = candidates[0]
        containers = [c for c in pod["status"].get("containerStatuses", []) if c["name"] == container_name]
        if len(containers) != 1 or not containers[0].get("ready"):
            raise Failure("expected Budget container is not ready")
        image = containers[0].get("imageID", "")
        if "@sha256:" not in image:
            raise Failure("observed container digest required")
        desired = [c for c in spec["template"]["spec"]["containers"] if c["name"] == container_name]
        actual = [c for c in pod["spec"]["containers"] if c["name"] == container_name]
        if (len(desired) != 1 or len(actual) != 1 or desired[0]["image"] != actual[0]["image"]
                or desired[0].get("env", []) != actual[0].get("env", [])):
            raise Failure("ready pod does not match the current deployment image/environment")
        result[name] = {"pod": pod["metadata"]["name"], "uid": pod["metadata"]["uid"],
                        "image": image.removeprefix("docker-pullable://"),
                        "container": container_name,
                        "containers": spec["template"]["spec"]["containers"]}
    flux = kube_json(kube, "-n", "flux-system", "get",
                     "kustomization.kustomize.toolkit.fluxcd.io", "flux-system")
    result["revision"] = flux_revision(flux)
    result["flux_generation"] = flux["metadata"]["generation"]
    result["flux_uid"] = flux["metadata"].get("uid")
    return result


def private_json(path, value):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as stream:
        json.dump(value, stream, indent=2)
        stream.write("\n")


def native_wrapper(path, kube, namespace, pod, container, tool):
    # PostgreSQL tools execute in the pinned existing pod, over its trusted local
    # socket. No credential, DSN or passfile is passed into kubectl argv.
    context_env = {"HOME": os.environ.get("HOME", "")}
    if "KUBECONFIG" in os.environ:
        context_env["KUBECONFIG"] = os.environ["KUBECONFIG"]
    source = """#!/usr/bin/env python3
import os,subprocess,sys
os.environ.update(%r)
base = %r
tool = %r
if sys.argv[1:] == ['--version']:
    args = [*base, tool, '--version']
else:
    args = [*base, 'env', 'PGHOST=/var/run/postgresql', 'PGPORT=5432',
            'PGUSER='+os.environ['PGUSER'], 'PGDATABASE='+os.environ['PGDATABASE'],
            'PGSSLMODE=disable', tool, *sys.argv[1:]]
raise SystemExit(subprocess.call(args))
""" % (context_env, [*kube, "-n", namespace, "exec", "-i", pod, "-c", container, "--"], tool)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o700), "w") as stream:
        stream.write(source)


def stop(process):
    if process is not None and process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            process.wait(timeout=5)
            return
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)


def run(args):
    binary = Path(args.budgetbackup).resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise Failure("reviewed executable budgetbackup binary is required")
    binary_sha = hashlib.sha256(binary.read_bytes()).hexdigest()
    if binary_sha != args.binary_sha256:
        raise Failure("budgetbackup binary SHA256 mismatch")
    kube = [args.kubectl] if args.kubectl else ["k3s", "kubectl"]
    if not shutil.which(kube[0]):
        raise Failure("k3s/kubectl executable is unavailable")
    out = Path(args.out).absolute()
    out.mkdir(mode=0o700)  # No overwrite or reuse of a previous/partial run.
    os.chmod(out, 0o700)
    forward = None
    try:
        observed = inventory(kube, args.namespace)
        database = observed["budget-postgres"]
        containers = [c for c in database["containers"] if c["name"] == database["container"]]
        if len(containers) != 1:
            raise Failure("PostgreSQL container configuration is ambiguous")
        variables = {v["name"]: v for v in containers[0].get("env", [])}
        user, db = (variables.get(k, {}).get("value", "") for k in ("POSTGRES_USER", "POSTGRES_DB"))
        ref = variables.get("POSTGRES_PASSWORD", {}).get("valueFrom", {}).get("secretKeyRef", {})
        if not user or not db or not ref.get("name") or not ref.get("key"):
            raise Failure("PostgreSQL user/database and password Secret reference are required")
        secret = kube_json(kube, "-n", args.namespace, "get", "secret", ref["name"])
        try:
            password = base64.b64decode(secret["data"][ref["key"]], validate=True).decode()
        except (KeyError, ValueError, UnicodeError):
            raise Failure("PostgreSQL Secret cannot be decoded") from None
        # Secret contents are never serialized, printed or put into command args.
        del secret
        pod = database["pod"]
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        forward = subprocess.Popen([*kube, "-n", args.namespace, "port-forward", "--address=127.0.0.1",
                                    "pod/" + pod, str(port) + ":5432"],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   start_new_session=True)
        deadline = time.monotonic() + 30
        while True:
            if forward.poll() is not None:
                raise Failure("owned loopback PostgreSQL port-forward exited")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                    break
            except OSError:
                if time.monotonic() >= deadline:
                    raise Failure("owned loopback PostgreSQL port-forward timed out")
                time.sleep(0.2)
        # Validate that the pod did not change between inventory and connection.
        if inventory(kube, args.namespace) != observed:
            raise Failure("Budget deployment/pod/Flux state changed during preflight")
        dsn = "postgresql://" + urllib.parse.quote(user, safe="") + ":" + urllib.parse.quote(password, safe="")
        dsn += "@127.0.0.1:" + str(port) + "/" + urllib.parse.quote(db, safe="") + "?sslmode=disable"
        del password
        env = {k: v for k, v in os.environ.items() if k not in ("DATABASE_URL", "TARGET_ADMIN_DATABASE_URL")}
        dump, restore = out / "pg_dump", out / "pg_restore"
        for path, tool in ((dump, "pg_dump"), (restore, "pg_restore")):
            native_wrapper(path, kube, args.namespace, pod, database["container"], tool)
        backup_dir = Path(args.existing_backup).absolute() if args.existing_backup else out / "backup"
        common = [str(binary), "-directory", str(backup_dir), "-pg-dump", str(dump), "-pg-restore", str(restore)]
        release = {"BackendImage": observed["budget-app"]["image"], "WebImage": observed["budget-web"]["image"],
                   "DatabaseImage": database["image"], "Revision": observed["revision"]}
        # Run without a shell; fixed library errors are safe, but suppress even
        # those here so an unexpected external diagnostic cannot leak data.
        if args.existing_backup:
            command([str(binary), "-mode", "verify", "-directory", str(backup_dir),
                     "-expected-manifest-sha", args.manifest_sha256], env=env,
                    timeout=1900, stage="existing backup verification", safe_errors=True)
        else:
            command([*common, "-mode", "backup", "-backend-image", release["BackendImage"],
                     "-web-image", release["WebImage"], "-database-image", release["DatabaseImage"],
                     "-revision", release["Revision"]], env={**env, "DATABASE_URL": dsn}, timeout=1900,
                    stage="full backup", safe_errors=True)
        manifest_path = backup_dir / "manifest.json"
        manifest_raw = manifest_path.read_bytes()
        manifest = json.loads(manifest_raw)
        if manifest["State"]["Migration"] != {"Version": args.expected_schema, "Dirty": False}:
            raise Failure("backup schema differs from the expected clean production version; restore not started")
        if inventory(kube, args.namespace) != observed:
            raise Failure("Budget deployment/pod/Flux state changed during backup; restore not started")
        manifest_sha = hashlib.sha256(manifest_raw).hexdigest()
        target = "budget_restore_" + time.strftime("%Y%m%d_%H%M%S", time.gmtime()) + "_" + uuid.uuid4().hex[:12]
        private_json(out / "restore-intent.json", {"TargetDatabase": target, "ManifestSHA256": manifest_sha,
                                                  "UpgradeRehearsal": args.rehearse_upgrade})
        mode = "rehearse-upgrade" if args.rehearse_upgrade else "restore"
        command([*common, "-mode", mode, "-expected-manifest-sha", manifest_sha,
                 "-target-name", target, "-report", str(out / "restore-report.json")],
                env={**env, "TARGET_ADMIN_DATABASE_URL": dsn}, timeout=1900,
                stage="isolated upgrade rehearsal" if args.rehearse_upgrade else "isolated restore", safe_errors=True)
        del dsn
        report = json.loads((out / "restore-report.json").read_text())
        upgrade = report if args.rehearse_upgrade else None
        if upgrade:
            report = upgrade["Restore"]
        if inventory(kube, args.namespace) != observed:
            raise Failure("restore completed but deployment/pod/Flux state changed; inventory needs fresh review")
        summary = {"Status": "backup_and_restore_verified", "SourceWrites": False,
                   "SchemaVersion": report["State"]["Migration"]["Version"],
                   "VerifiedTables": report["VerifiedTables"], "VerifiedRows": report["VerifiedRows"],
                   "ManifestSHA256": manifest_sha, "BinarySHA256": binary_sha,
                   "Release": report["Release"], "ObservedRelease": release,
                   "ReusedBackup": bool(args.existing_backup),
                   "StartedAt": manifest["StartedAt"], "CompletedAt": report["CompletedAt"],
                   "RestoreDatabase": target, "RestoreDatabaseRetained": True}
        if upgrade:
            summary.update(Status="isolated_upgrade_verified", SourceSchemaVersion=summary["SchemaVersion"],
                           SchemaVersion=upgrade["State"]["Migration"]["Version"],
                           PreservedTables=upgrade["PreservedTables"], PreservedRows=upgrade["PreservedRows"],
                           RestartVerified=upgrade["RestartVerified"], CompletedAt=upgrade["CompletedAt"])
        private_json(out / "operator-report.json", summary)
        print(json.dumps(summary, indent=2), flush=True)
    finally:
        stop(forward)


def main():
    def terminate(_signum, _frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, terminate)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--budgetbackup", required=True, help="reviewed native binary path")
    parser.add_argument("--binary-sha256", required=True, help="reviewed binary checksum")
    parser.add_argument("--out", required=True, help="new private evidence directory on the operator host")
    parser.add_argument("--namespace", default="budget")
    parser.add_argument("--expected-schema", type=int, default=11)
    parser.add_argument("--kubectl", help="kubectl executable; default: k3s kubectl")
    parser.add_argument("--existing-backup", help="private existing backup directory; skip a new source dump")
    parser.add_argument("--manifest-sha256", help="reviewed checksum required with --existing-backup")
    parser.add_argument("--rehearse-upgrade", action="store_true",
                        help="restore a new isolated clone and rehearse embedded migrations; existing backup required")
    args = parser.parse_args()
    if bool(args.existing_backup) != bool(args.manifest_sha256):
        parser.error("--existing-backup and --manifest-sha256 are required together")
    if args.rehearse_upgrade and not args.existing_backup:
        parser.error("--rehearse-upgrade requires --existing-backup and --manifest-sha256")
    if args.manifest_sha256 and (len(args.manifest_sha256) != 64 or any(c not in "0123456789abcdef" for c in args.manifest_sha256)):
        parser.error("--manifest-sha256 must be a lowercase SHA256")
    try:
        run(args)
    except Failure as error:
        print(str(error) + ". No operator completion certificate was issued.", file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, TypeError, KeyboardInterrupt):
        # Never print repr/traceback of subprocess, JSON or environment data.
        print("Backup/restore gate failed or interrupted. No completion certificate was issued. "
              "Existing backup/restore-intent files are retained; do not rerun into the same directory.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
