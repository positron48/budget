#!/usr/bin/env python3
"""Rebuild reviewed standalone portfolios, preserving household Budget data.

Default: full verified backup, isolated candidate and rolled-back replacement.
--apply: commit the same checked replacement. Credentials remain in-process.
Private source/market inputs are operator-supplied, never embedded in this script.
"""
import argparse
from contextlib import contextmanager, redirect_stdout
import hashlib
import io
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
from types import SimpleNamespace
import uuid

import prepare_portfolio as preparation
import production_backup as backup


class Failure(Exception):
    pass


def checked_file(path, expected):
    path = Path(path).resolve(strict=True)
    if not path.is_file() or path.stat().st_mode & 0o077:
        raise Failure("operator inputs and binaries must be private regular files")
    if hashlib.sha256(path.read_bytes()).hexdigest() != expected:
        raise Failure("reviewed file checksum mismatch")
    return path


@contextmanager
def connection(kube, namespace):
    dsn = preparation.deployment_dsn(kube, namespace)
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    process = subprocess.Popen(kube + ["-n", namespace, "port-forward", "service/budget-postgres",
                                      str(port) + ":5432", "--address=127.0.0.1"],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
    try:
        deadline = time.monotonic() + 20
        while True:
            if process.poll() is not None:
                raise Failure("database port-forward exited")
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                    break
            except OSError:
                if time.monotonic() >= deadline:
                    raise Failure("database port-forward timed out")
                time.sleep(0.2)
        yield preparation.forwarded_dsn(dsn, port)
    finally:
        backup.stop(process)


def require_report(report, scope):
    if (report.get("Status") != "isolated_portfolio_candidate_prepared"
            or report.get("SourceWrites") is not False
            or report.get("SchemaVersion") != 61
            or report.get("BeforeStateSHA256") != scope["StateSHA256"]
            or report.get("Accounts") != len(scope["Scope"]["Accounts"])
            or report.get("Operations", 0) <= 0
            or report.get("FreshQuotes", 0) <= 0
            or report.get("FXChecks", 0) <= 0):
        raise Failure("isolated candidate report does not match the reviewed source scope")


def run(args):
    os.umask(0o077)
    source = checked_file(args.plan, args.plan_sha256)
    market = checked_file(args.market, args.market_file_sha256)
    archive_binary = checked_file(args.archive_binary, args.archive_binary_sha256)
    candidate_binary = checked_file(args.candidate_binary, args.candidate_binary_sha256)
    backup_binary = checked_file(args.backup_binary, args.backup_binary_sha256)
    if not all(os.access(p, os.X_OK) for p in (archive_binary, candidate_binary, backup_binary)):
        raise Failure("operator binaries must be executable")
    out = Path(args.output).absolute()
    out.mkdir(mode=0o700, parents=False, exist_ok=False)
    kube = [args.kubectl] if args.kubectl else ["k3s", "kubectl"]
    print("1/5: full backup and isolated restore verification", flush=True)
    with redirect_stdout(io.StringIO()):
        backup.run(SimpleNamespace(budgetbackup=str(backup_binary), binary_sha256=args.backup_binary_sha256,
                                   out=str(out / "database-backup"), namespace=args.namespace,
                                   expected_schema=61, kubectl=args.kubectl, existing_backup=None,
                                   manifest_sha256=None, rehearse_upgrade=False))
    backup_report = json.loads((out / "database-backup/operator-report.json").read_text())
    if backup_report.get("Status") != "backup_and_restore_verified" or backup_report.get("SourceWrites") is not False:
        raise Failure("full source backup/restore was not verified")
    observed = backup.inventory(kube, args.namespace)
    print("2/5: inspect actual account identities and export current history", flush=True)
    with redirect_stdout(io.StringIO()):
        preparation.run(SimpleNamespace(output=str(out / "source"), binary=str(archive_binary),
                                        namespace=args.namespace, kubectl=args.kubectl, ledger=args.ledger))
    scope = json.loads((out / "source/scope.json").read_text())
    if scope["Scope"]["SchemaVersion"] != 61 or scope["Scope"]["SchemaDirty"]:
        raise Failure("clean deployed schema61 required")
    with connection(kube, args.namespace) as dsn:
        env = {k: v for k, v in os.environ.items() if k not in (
            "DATABASE_URL", "TARGET_ADMIN_DATABASE_URL", "PORTFOLIO_SOURCE_ADMIN_URL")}
        env.update(DATABASE_URL=dsn, PORTFOLIO_SOURCE_ADMIN_URL=dsn)
        candidate_file, report_file = out / "candidate.json", out / "candidate-report.json"
        database = "budget_portfolio_candidate_" + uuid.uuid4().hex
        print("3/5: build separate candidate and verify actual quantities, costs, FX and quotes", flush=True)
        preparation.command([str(candidate_binary), "-before", str(out / "source/before-investments.json"),
                             "-plan", str(source), "-market", str(market), "-actor", scope["Scope"]["ActorID"],
                             "-database-name", database, "-output", str(candidate_file), "-report", str(report_file)],
                            "isolated portfolio candidate", env, timeout=950)
        report = json.loads(report_file.read_text())
        require_report(report, scope)
        command = [str(archive_binary), "-actor", scope["Scope"]["ActorID"],
                   "-expected-sha", report["ArchiveSHA256"], "-before-state-sha256", scope["StateSHA256"],
                   "-market-file", str(market), "-expected-market-sha256", report["MarketSHA256"]]
        print("4/5: rehearse complete replacement in a rolled-back transaction", flush=True)
        preparation.command(command + ["-check-replace", str(candidate_file)],
                            "portfolio replacement rehearsal", env, timeout=240)
        if backup.inventory(kube, args.namespace) != observed:
            raise Failure("deployment changed during candidate construction; replacement refused")
        if args.apply:
            print("5/5: commit verified portfolio and market data together", flush=True)
            preparation.command(command + ["-replace", str(candidate_file)],
                                "atomic portfolio replacement", env, timeout=240)
        else:
            print("5/5: rehearsal complete; production investment history unchanged", flush=True)
        result = {"Status": "portfolio_replaced" if args.apply else "portfolio_replacement_rehearsed",
                  "SourceWrites": bool(args.apply), "HouseholdWrites": False,
                  "BackupManifestSHA256": backup_report["ManifestSHA256"],
                  "CandidateDatabase": database, "CandidateDatabaseRetained": True,
                  **{k: report[k] for k in ("ArchiveSHA256", "MarketSHA256", "Accounts", "Operations",
                                           "Payouts", "PendingSourceRows", "FreshQuotes", "RequiredCurrencyDates", "FXChecks")}}
        preparation.private_json(out / "operator-report.json", result)
        print(json.dumps(result, indent=2), flush=True)


def main():
    def interrupt(_signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupt)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True)
    parser.add_argument("--ledger", required=True)
    parser.add_argument("--namespace", default="budget")
    parser.add_argument("--kubectl")
    parser.add_argument("--apply", action="store_true")
    for name in ("archive-binary", "candidate-binary", "backup-binary", "plan"):
        parser.add_argument("--" + name, required=True)
        parser.add_argument("--" + name + "-sha256", required=True)
    parser.add_argument("--market", required=True)
    parser.add_argument("--market-file-sha256", required=True)
    args = parser.parse_args()
    try:
        run(args)
    except (Failure, preparation.Failure, backup.Failure) as error:
        print("Portfolio rebuild stopped: " + str(error) + ". Private evidence retained.", file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, StopIteration, KeyboardInterrupt):
        print("Portfolio rebuild stopped; private evidence retained. Inspect the saved report before retrying.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
