#!/usr/bin/env python3
"""Provision existing read-only provider keys for GitOps portfolio workers.

Keys are entered through a real hidden terminal prompt, sent to Kubernetes on
stdin, and never written to a local file, command arguments or the result report.
This command does not deploy services or change financial history.
"""
import argparse
import base64
import getpass
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import uuid
import warnings
import time
import math

SECRET = "budget-investment-refresh"
OWNER = "budget-investment-workers"


class Failure(Exception):
    pass


def command(argv, stage, data=None, timeout=30):
    try:
        result = subprocess.run(argv, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise Failure(stage + " could not finish") from error
    if result.returncode:
        raise Failure(stage + " failed; raw stderr withheld")
    return result.stdout


def hidden_key(prompt, maximum):
    if not sys.stdin.isatty() or not sys.stderr.isatty():
        raise Failure("an interactive terminal is required; visible key input is never accepted")
    with warnings.catch_warnings():
        warnings.simplefilter("error", getpass.GetPassWarning)
        try:
            key = getpass.getpass(prompt).strip()
        except (getpass.GetPassWarning, EOFError) as error:
            raise Failure("hidden provider key input unavailable") from error
    if not key or len(key) > maximum or not key.isascii() or any(c.isspace() for c in key):
        raise Failure("invalid provider key format")
    return base64.b64encode(key.encode()).decode()


def source_tenant(path):
    path = Path(path)
    info = path.stat()
    if not path.is_file() or info.st_mode & 0o077 or info.st_size > 1 << 20:
        raise Failure("private portfolio scope file required")
    report = json.loads(path.read_text())
    scope = report["Scope"]
    tenant = scope["TenantID"]
    if (report.get("Status") != "portfolio_scope_backed_up" or report.get("SourceWrites") is not False
            or scope["SchemaDirty"] or scope["SchemaVersion"] not in (61, 62, 63)
            or not scope["Accounts"] or any(a.get("PortfolioTracking") is not True for a in scope["Accounts"])
            or str(uuid.UUID(tenant)) != tenant):
        raise Failure("matching clean standalone portfolio scope required")
    return tenant


def provision(kube, namespace, tenant, native=False):
    raw = command(kube + ["-n", namespace, "get", "secret", SECRET, "--ignore-not-found", "-o", "json"],
                  "inspect worker credential Secret")
    current = json.loads(raw) if raw.strip() else None
    if current:
        if (current.get("metadata", {}).get("labels", {}).get("app.kubernetes.io/part-of") != OWNER
                or current.get("type") != "Opaque" or current.get("immutable")):
            raise Failure("existing Secret is not a mutable portfolio worker Secret")
        stored = base64.b64decode(current.get("data", {}).get("TENANT_ID", ""), validate=True).decode()
        if stored and stored != tenant:
            raise Failure("existing worker Secret belongs to a different portfolio scope")
    data = dict(current.get("data", {})) if current else {}
    wanted_tenant = base64.b64encode(tenant.encode()).decode()
    changed = data.get("TENANT_ID") != wanted_tenant
    data["TENANT_ID"] = wanted_tenant
    required = [("TINVEST_TOKEN", "Read-only T-Invest token (hidden): ", 8192),
                ("ALPHAVANTAGE_API_KEY", "Alpha Vantage API key (hidden): ", 256)]
    if native:
        required.append(("TWELVEDATA_API_KEY", "Twelve Data API key (hidden): ", 256))
    for key, prompt, maximum in required:
        if not data.get(key):
            data[key] = hidden_key(prompt, maximum)
            changed = True
    if changed:
        metadata = {"name": SECRET, "namespace": namespace,
                    "labels": {"app.kubernetes.io/part-of": OWNER}}
        if current:
            metadata = dict(current["metadata"])
            metadata.pop("managedFields", None)
            # Never create a last-applied annotation containing credentials.
            metadata.get("annotations", {}).pop("kubectl.kubernetes.io/last-applied-configuration", None)
        payload = {"apiVersion": "v1", "kind": "Secret", "metadata": metadata,
                   "type": "Opaque", "data": data}
        command(kube + ["-n", namespace, "replace" if current else "create", "-f", "-"],
                "provision portfolio worker credentials", json.dumps(payload).encode())
    return {"Status": "portfolio_worker_credentials_provisioned", "SecretCreated": current is None,
            "SecretUpdated": changed and current is not None, "ExistingCredentialsReused": not changed,
            "DeploymentWrites": False, "FinancialWrites": False, "CredentialsPrinted": False,
            "Providers": ["tinvest", "alphavantage"] + (["moex", "twelvedata"] if native else []), "WorkersReadyVerified": False}


def metric(body, name, wanted):
    total = 0.0
    found = False
    for line in body.splitlines():
        match = re.fullmatch(re.escape(name) + r'(?:\{([^}]*)\})?\s+([^\s]+)', line)
        if not match:
            continue
        labels = dict(re.findall(r'([a-zA-Z_][a-zA-Z_0-9]*)="([^"\\]*)"', match[1] or ""))
        if any(labels.get(key) != value for key, value in wanted.items()):
            continue
        try:
            value = float(match[2])
        except ValueError:
            return None
        if not math.isfinite(value) or value < 0:
            return None
        total += value
        found = True
    return total if found else None


def worker_status(kube, namespace, native=False):
    deadline = time.monotonic() + 20
    def read(argv, stage):
        left = deadline - time.monotonic()
        if left <= 0:
            raise Failure("worker diagnostic snapshot timed out")
        return command(argv, stage, timeout=min(10, left))
    base = kube + ["-n", namespace]
    app = json.loads(read(base + ["get", "deployment", "budget-app", "-o", "json"], "inspect current application image"))
    expected_image = next(c["image"] for c in app["spec"]["template"]["spec"]["containers"] if c["name"] == "budget-app")
    rows = []
    targets = [("marketdata", "marketdata"), ("alphafx", "alphafx"), ("cbr", "investcbr")]
    if native:
        targets.extend([("moexhistory", "moexhistory"), ("equityhistory", "equityhistory")])
    for suffix, worker in targets:
        name = "budget-portfolio-" + suffix
        pods = json.loads(read(base + ["get", "pods", "-l", "app=" + name, "-o", "json"], "inspect worker readiness"))["items"]
        pods = [p for p in pods if not p["metadata"].get("deletionTimestamp")]
        row = {"Worker": worker, "Ready": False, "SuccessfulUpdates": 0, "SnapshotReadable": False}
        if len(pods) == 1:
            pod = pods[0]
            containers = pod["spec"]["containers"]
            statuses = pod.get("status", {}).get("containerStatuses", [])
            row["Ready"] = (len(containers) == 1 and containers[0]["image"] == expected_image
                            and len(statuses) == 1 and statuses[0].get("ready") is True
                            and any(c.get("type") == "Ready" and c.get("status") == "True"
                                    for c in pod.get("status", {}).get("conditions", [])))
            if row["Ready"]:
                path = "/api/v1/namespaces/" + namespace + "/pods/" + pod["metadata"]["name"] + ":9091/proxy/metrics"
                body = read(kube + ["get", "--raw", path], "read non-financial worker diagnostics").decode()
                row["SnapshotReadable"] = metric(body, "budget_investment_worker_snapshot_success", {"worker": worker}) == 1
                if worker in ("moexhistory", "equityhistory"):
                    row["SuccessfulUpdates"] = metric(body, "budget_investment_worker_job_attempts_total", {"worker": worker, "kind": "daily_candle", "result": "ready"}) or 0
                    row["SourceEnabled"] = metric(body, "budget_investment_worker_source_enabled", {"worker": worker}) == 1
                elif worker == "marketdata":
                    row["SuccessfulUpdates"] = metric(body, "budget_investment_worker_job_attempts_total", {"worker": worker, "kind": "last_price", "result": "ready"}) or 0
                    row["SourceEnabled"] = metric(body, "budget_investment_worker_source_enabled", {"worker": worker}) == 1
                    row["FreshMappedQuotes"] = metric(body, "budget_investment_worker_mapped_quote_endpoints", {"worker": worker, "state": "fresh"}) or 0
                    invalid = [metric(body, "budget_investment_worker_mapped_quote_endpoints", {"worker": worker, "state": state}) for state in ("expired", "missing")]
                    row["UnusableMappedQuotes"] = sum(invalid) if all(v is not None for v in invalid) else None
                else:
                    row["SuccessfulUpdates"] = metric(body, "budget_investment_worker_iterations_total", {"worker": worker, "result": "success"}) or 0
                if worker == "alphafx":
                    row["FreshFXPairs"] = metric(body, "budget_investment_worker_fx_endpoints", {"worker": worker, "state": "fresh"}) or 0
                    invalid = [metric(body, "budget_investment_worker_fx_endpoints", {"worker": worker, "state": state}) for state in ("missing", "expired")]
                    row["MissingOrExpiredFXPairs"] = sum(invalid) if all(v is not None for v in invalid) else None
        rows.append(row)
    verified = all(r["Ready"] and r["SnapshotReadable"] and r["SuccessfulUpdates"] > 0 for r in rows)
    verified = verified and rows[0].get("SourceEnabled") is True and rows[0].get("FreshMappedQuotes", 0) > 0 and rows[0].get("UnusableMappedQuotes") == 0 and rows[1].get("FreshFXPairs") == 2 and rows[1].get("MissingOrExpiredFXPairs") == 0
    if native:
        verified = verified and all(r.get("SourceEnabled") is True for r in rows[3:])
    return {"WorkersOperationalVerified": verified, "WorkersReadyVerified": all(r["Ready"] for r in rows), "Workers": rows,
            "FullQuoteCoverageVerified": False, "FinancialWrites": False}


def wait_workers(kube, namespace, seconds, native=False):
    deadline = time.monotonic() + seconds
    next_notice = time.monotonic()
    latest = {"WorkersOperationalVerified": False}
    while True:
        try:
            latest = worker_status(kube, namespace, native=native)
        except (Failure, ValueError, KeyError, StopIteration):
            latest = {"WorkersOperationalVerified": False, "DiagnosticsReadable": False}
        if latest["WorkersOperationalVerified"] or time.monotonic() >= deadline:
            return latest
        if time.monotonic() >= next_notice:
            ready = sum(r.get("Ready") is True for r in latest.get("Workers", []))
            print("Waiting for portfolio workers: ready=" + str(ready) + ("/5" if native else "/3") + "; provider and freshness checks pending", file=sys.stderr, flush=True)
            next_notice = time.monotonic() + 30
        time.sleep(min(5, max(0, deadline-time.monotonic())))


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scope-file", required=True)
    parser.add_argument("--namespace", default="budget")
    parser.add_argument("--kubectl")
    parser.add_argument("--check-only", action="store_true", help="read readiness and successful provider updates; no Secret read/write")
    parser.add_argument("--native", action="store_true", help="also provision and verify the native history workers")
    parser.add_argument("--wait-seconds", type=int, default=0)
    args = parser.parse_args()
    try:
        if not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", args.namespace):
            raise Failure("valid Kubernetes namespace required")
        if args.wait_seconds < 0 or args.wait_seconds > 600:
            raise Failure("valid bounded worker wait and compatible mode required")
        kube = [args.kubectl] if args.kubectl else ["k3s", "kubectl"]
        tenant = source_tenant(args.scope_file)
        result = {"Status": "portfolio_workers_checked", "FinancialWrites": False} if args.check_only else provision(kube, args.namespace, tenant, native=args.native)
        if args.check_only or args.wait_seconds:
            result.update(wait_workers(kube, args.namespace, args.wait_seconds, native=args.native))
        print(json.dumps(result, indent=2))
    except Failure as error:
        print("Worker provisioning: " + str(error), file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, TypeError, KeyboardInterrupt):
        print("Worker provisioning stopped; credential and configuration details withheld", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
