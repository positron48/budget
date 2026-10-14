#!/usr/bin/env python3
"""Read-only server preparation; credentials stay inside the local process."""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import time
import urllib.parse


class Failure(Exception):
    pass


def command(argv, stage, env=None, timeout=90):
    try:
        result = subprocess.run(argv, env=env, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise Failure(stage + " could not finish") from error
    if result.returncode:
        raise Failure(stage + " failed; raw stderr withheld")
    return result.stdout


def deployment_dsn(kube, namespace):
    def read(*args):
        return json.loads(command(kube + ["-n", namespace, "get", *args, "-o", "json"], "read Kubernetes configuration"))
    deployment = read("deployment", "budget-app")
    containers = deployment["spec"]["template"]["spec"]["containers"]
    app = next(c for c in containers if c["name"] == "budget-app")
    variable = next(v for v in app.get("env", []) if v["name"] == "DATABASE_URL")
    sources = variable.get("valueFrom", {})
    if "secretKeyRef" in sources:
        reference = sources["secretKeyRef"]
        secret = read("secret", reference["name"])
        return base64.b64decode(secret["data"][reference["key"]], validate=True).decode()
    if "configMapKeyRef" in sources:
        reference = sources["configMapKeyRef"]
        config = read("configmap", reference["name"])
        return config["data"][reference["key"]]
    raise Failure("DATABASE_URL must reference an existing Secret or ConfigMap")


def forwarded_dsn(original, port):
    url = urllib.parse.urlsplit(original)
    if url.scheme not in ("postgres", "postgresql") or not url.hostname or not url.username or not url.path:
        raise Failure("DATABASE_URL must be a PostgreSQL URL")
    auth = urllib.parse.quote(urllib.parse.unquote(url.username), safe="")
    if url.password is not None:
        auth += ":" + urllib.parse.quote(urllib.parse.unquote(url.password), safe="")
    return urllib.parse.urlunsplit((url.scheme, auth + "@127.0.0.1:" + str(port), url.path, url.query, ""))


def private_json(path, value):
    with path.open("x", encoding="utf-8") as file:
        os.chmod(path, 0o600)
        json.dump(value, file, ensure_ascii=False, indent=2)
        file.write("\n")


def run(args):
    os.umask(0o077)
    out = Path(args.output).resolve()
    out.mkdir(mode=0o700, parents=False, exist_ok=False)
    binary = Path(args.binary).resolve()
    if not binary.is_file():
        raise Failure("operator binary missing")
    kube = [args.kubectl] if args.kubectl else ["k3s", "kubectl"]
    dsn = deployment_dsn(kube, args.namespace)
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    forward = subprocess.Popen(kube + ["-n", args.namespace, "port-forward",
                                      "service/budget-postgres", str(port) + ":5432", "--address=127.0.0.1"],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
    try:
        deadline = time.monotonic() + 20
        while True:
            if forward.poll() is not None:
                raise Failure("database port-forward exited")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                    break
            except OSError:
                if time.monotonic() >= deadline:
                    raise Failure("database port-forward did not become ready")
                time.sleep(0.2)
        env = dict(os.environ, DATABASE_URL=forwarded_dsn(dsn, port))
        del dsn
        scope = json.loads(command([str(binary), "-scope-ledger", args.ledger], "inspect portfolio scope", env))
        if scope["SchemaDirty"]:
            raise Failure("database migration is dirty")
        archive_path = out / "before-investments.json"
        summary = command([str(binary), "-tenant", scope["TenantID"], "-actor", scope["ActorID"],
                           "-output", str(archive_path)], "export existing investment history", env, 180).decode()
        match = re.search(r"SHA256 ([0-9a-f]{64}); state SHA256 ([0-9a-f]{64})", summary)
        if not match:
            raise Failure("archive export did not return its fingerprints")
        # Take account identities from the same exported snapshot as the state digest.
        with archive_path.open(encoding="utf-8") as file:
            archive = json.load(file)
        if archive["Data"]["TenantID"] != scope["TenantID"]:
            raise Failure("archive scope mismatch")
        accounts = next(t["Rows"] for t in archive["Data"]["Tables"] if t["Name"] == "investment_ledgers")
        scope["Accounts"] = [{"ID": a["id"], "Name": a["name"], "Version": a["version"],
                              "MetadataVersion": a["metadata_version"], "PortfolioTracking": a["portfolio_tracking"]}
                             for a in accounts]
        verified = True
        try:
            command([str(binary), "-verify", str(archive_path)], "verify existing archive journal", timeout=180)
        except Failure:
            verified = False
        result = {"Status": "portfolio_scope_backed_up", "SourceWrites": False,
                  "ArchiveSHA256": match.group(1), "StateSHA256": match.group(2),
                  "BackupJournalVerified": verified, "Scope": scope}
        private_json(out / "scope.json", result)
        print(json.dumps(result, ensure_ascii=False, indent=2))
    finally:
        if forward.poll() is None:
            os.killpg(forward.pid, signal.SIGTERM)
            try:
                forward.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(forward.pid, signal.SIGKILL)
                forward.wait(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True)
    parser.add_argument("--ledger", required=True)
    parser.add_argument("--binary", default="./investmentarchive-linux-amd64")
    parser.add_argument("--namespace", default="budget")
    parser.add_argument("--kubectl")
    args = parser.parse_args()
    try:
        run(args)
    except Failure as error:
        print("Portfolio preparation: " + str(error) + ". Private files retained; no database writes requested.", file=__import__("sys").stderr)
        return 1
    except (OSError, ValueError, KeyError, StopIteration):
        print("Portfolio preparation failed; private files retained, no database writes requested.", file=__import__("sys").stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
