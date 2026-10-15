#!/usr/bin/env python3
"""Disposable authenticated browser -> Envoy -> budgetd -> PostgreSQL smoke.

Never accepts an existing DB URL, broker credential or deployment target. All
containers and processes belong to this run and are removed in finally.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]


def run(*args, **kwargs):
    check = kwargs.pop("check", True)
    timeout = kwargs.pop("timeout", 180)
    if kwargs.pop("capture_output", False):
        kwargs.update(stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    proc = subprocess.Popen(args, text=True, start_new_session=True, **kwargs)
    try:
        stdout, stderr = proc.communicate(timeout=timeout)
    except BaseException:
        stop(proc)
        raise
    result = subprocess.CompletedProcess(args, proc.returncode, stdout, stderr)
    if check:
        result.check_returncode()
    return result


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def stop(proc):
    if proc and proc.poll() is None:
        os.killpg(proc.pid, signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=5)


def wait_tcp(port, proc=None):
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        if proc and proc.poll() is not None:
            raise RuntimeError("owned server exited before readiness; inspect run logs")
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.25):
                return
        except OSError:
            time.sleep(0.2)
    raise RuntimeError("local server readiness deadline exceeded")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", help="new directory for logs/evidence (must not exist)")
    parser.add_argument("--reuse-build", action="store_true", help="reuse local Next build; backend is always rebuilt")
    args = parser.parse_args()
    out = Path(args.out).resolve() if args.out else Path(tempfile.mkdtemp(prefix="budget-fullstack-"))
    if args.out:
        out.mkdir(mode=0o700)
    os.chmod(out, 0o700)
    # The frontend build embeds this address; fail if another local server owns it.
    for port in [3030, 8081]:
        with socket.socket() as sock:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind(("127.0.0.1", port))
    names, processes, files = [], [], []
    run_id = uuid.uuid4().hex
    env = dict(os.environ)
    env.update(NEXT_PUBLIC_GRPC_BASE_URL="http://127.0.0.1:3030/grpc",
               INVESTMENTS_ENABLED="true", INVESTMENTS_READ_ONLY="false",
               ENVOY_URL="http://127.0.0.1:8081")

    def log(name):
        file = (out / name).open("w")
        files.append(file)
        return file

    def server(command, variables, filename, cwd=ROOT):
        proc = subprocess.Popen(command, cwd=cwd, env=variables, stdout=log(filename),
                                stderr=subprocess.STDOUT, start_new_session=True)
        processes.append(proc)
        return proc

    def container(label, image, port, *options):
        name = "budget-smoke-" + run_id[:12] + "-" + label
        names.append(name)
        run("docker", "run", "-d", "--name", name, "-p", f"127.0.0.1::{port}", *options, image,
            stdout=subprocess.DEVNULL)
        mapped = run("docker", "port", name, str(port), capture_output=True).stdout.strip()
        return name, int(mapped.rsplit(":", 1)[1])

    try:
        run("go", "build", "-trimpath", "-o", str(out / "budgetd"), "./cmd/budgetd", cwd=ROOT,
            env=env, stdout=log("go-build.log"), stderr=subprocess.STDOUT)
        if not args.reuse_build:
            run("npm", "run", "build", cwd=ROOT / "web", env=env,
                stdout=log("web-build.log"), stderr=subprocess.STDOUT)
        elif not (ROOT / "web/.next/BUILD_ID").is_file():
            raise RuntimeError("--reuse-build requires a completed Next build")
        manifest = json.loads((ROOT / "web/.next/routes-manifest.json").read_text())
        rewrites = manifest.get("rewrites", {})
        rules = rewrites if isinstance(rewrites, list) else sum(rewrites.values(), [])
        grpc = [r for r in rules if r.get("source") == "/grpc/:path*"]
        if len(grpc) != 1 or grpc[0].get("destination") not in ["http://127.0.0.1:8081/:path*", "http://localhost:8081/:path*"]:
            raise RuntimeError("Next build must route /grpc only to our local Envoy on port 8081")
        frontend = out / "web"
        shutil.copytree(ROOT / "web/.next/standalone", frontend)
        shutil.copytree(ROOT / "web/.next/static", frontend / ".next/static", dirs_exist_ok=True)
        if (ROOT / "web/public").is_dir():
            shutil.copytree(ROOT / "web/public", frontend / "public", dirs_exist_ok=True)
        pg, pg_port = container("pg", "postgres:16", 5432, "-e", "POSTGRES_USER=budget",
                                "-e", "POSTGRES_PASSWORD=smoke-disposable", "-e", "POSTGRES_DB=budget")
        redis, redis_port = container("redis", "redis:7-alpine", 6379)
        deadline = time.monotonic() + 45
        while run("docker", "exec", pg, "pg_isready", "-U", "budget", check=False,
                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
            if time.monotonic() > deadline:
                raise RuntimeError("disposable PostgreSQL readiness timeout")
            time.sleep(0.2)
        wait_tcp(redis_port)
        backend_port = free_port()
        backend_env = dict(env, APP_ENV="test", GRPC_ADDR=f"0.0.0.0:{backend_port}",
                           DATABASE_URL=f"postgres://budget:smoke-disposable@127.0.0.1:{pg_port}/budget?sslmode=disable",
                           REDIS_URL=f"redis://127.0.0.1:{redis_port}", JWT_SIGN_KEY=uuid.uuid4().hex,
                           JWT_ACCESS_TTL="1h", JWT_REFRESH_TTL="24h", AUTH_PASSWORD_ENABLED="true",
                           FX_IMPORT_ENABLED="false", METRICS_ADDR="", OTEL_EXPORTER_OTLP_ENDPOINT="",
                           INVESTMENTS_TENANT_ALLOWLIST="", GOOGLE_CLIENT_ID="",
                           OAUTH_AUTH_TOKEN_TTL="5m", OAUTH_SESSION_TTL="1h",
                           OAUTH_VERIFICATION_CODE_TTL="5m", OAUTH_MAX_ATTEMPTS_PER_HOUR="30",
                           OAUTH_MAX_ATTEMPTS_PER_10MIN="10", OAUTH_WEB_BASE_URL="http://127.0.0.1:3030")
        backend = server([str(out / "budgetd")], backend_env, "backend-write.log")
        wait_tcp(backend_port, backend)
        # Copy into our stopped container; works with remote Docker Desktop
        # daemons where host bind mounts are unavailable.
        envoy_name = "budget-smoke-" + run_id[:12] + "-envoy"
        names.append(envoy_name)
        config = (ROOT / "deploy/envoy.yaml").read_text()
        config = config.replace("address: 127.0.0.1, port_value: 8081", "address: 0.0.0.0, port_value: 8081")
        config = config.replace("type: static", "type: STRICT_DNS\n    dns_lookup_family: V4_ONLY")
        config = config.replace("address: 127.0.0.1, port_value: 8080",
                                f"address: host.docker.internal, port_value: {backend_port}")
        (out / "envoy.yaml").write_text(config)
        run("docker", "create", "--name", envoy_name, "--add-host", "host.docker.internal:host-gateway",
            "-p", "127.0.0.1:8081:8081", "envoyproxy/envoy:v1.36.7", "envoy", "-c", "/tmp/smoke-envoy.yaml",
            "--log-level", "warning", stdout=subprocess.DEVNULL)
        run("docker", "cp", str(out / "envoy.yaml"), envoy_name + ":/tmp/smoke-envoy.yaml")
        run("docker", "start", envoy_name, stdout=subprocess.DEVNULL)
        envoy_port = int(run("docker", "port", envoy_name, "8081", capture_output=True).stdout.strip().rsplit(":", 1)[1])
        wait_tcp(envoy_port)
        web_env = dict(env, PORT="3030", HOSTNAME="127.0.0.1", NODE_ENV="production", ENVOY_URL=f"http://127.0.0.1:{envoy_port}")
        web = server(["node", str(frontend / "server.js")], web_env, "web-server.log", frontend)
        wait_tcp(3030, web)
        for _ in range(100):
            try:
                with urllib.request.urlopen("http://127.0.0.1:3030/login", timeout=1):
                    break
            except OSError:
                time.sleep(0.2)
        browser_env = dict(env, E2E_FULLSTACK="1", E2E_BASE_URL="http://127.0.0.1:3030",
                           E2E_FULLSTACK_RUN=run_id)
        for phase in ["write", "restart", "readonly", "disabled"]:
            if phase != "write":
                stop(backend)
                backend_env.update(INVESTMENTS_READ_ONLY="true" if phase == "readonly" else "false",
                                   INVESTMENTS_ENABLED="false" if phase == "disabled" else "true")
                backend = server([str(out / "budgetd")], backend_env, f"backend-{phase}.log")
                wait_tcp(backend_port, backend)
                if phase in ["readonly", "disabled"]:
                    stop(web)
                    web_env.update(INVESTMENTS_READ_ONLY=backend_env["INVESTMENTS_READ_ONLY"],
                                   INVESTMENTS_ENABLED=backend_env["INVESTMENTS_ENABLED"])
                    web = server(["node", str(frontend / "server.js")], web_env, f"web-{phase}.log", frontend)
                    wait_tcp(3030, web)
            run("npx", "playwright", "test", "e2e/investment-fullstack.spec.ts", "--workers=1", "--reporter=line",
                cwd=ROOT / "web", env=dict(browser_env, E2E_FULLSTACK_PHASE=phase),
                stdout=log(f"browser-{phase}.log"), stderr=subprocess.STDOUT)
        query = "SELECT (SELECT version FROM schema_migrations), (SELECT count(*) FROM investment_ledgers), (SELECT count(*) FROM investment_operations), (SELECT count(*) FROM investment_payout_events), (SELECT count(*) FROM investment_payout_links);"
        counts = run("docker", "exec", pg, "psql", "-U", "budget", "-d", "budget", "-At", "-c", query,
                     capture_output=True).stdout.strip()
        if counts.split("|")[1:] != ["4", "6", "2", "2"]:
            raise RuntimeError("unexpected persisted row counts after feature/read-only checks")
        issuer_counts = run("docker", "exec", pg, "psql", "-U", "budget", "-d", "budget", "-At", "-c",
                            "SELECT (SELECT count(*) FROM investment_dividend_histories),(SELECT count(*) FROM investment_dividend_history_revisions);",
                            capture_output=True).stdout.strip()
        if issuer_counts != "2|4":
            raise RuntimeError("issuer history revisions did not survive restart/read-only checks")
        report = {"status": "passed", "scope": "disposable local authenticated full stack; no production or broker calls",
                  "backend_sha256": hashlib.sha256((out / "budgetd").read_bytes()).hexdigest(),
                  "web_build_id": (ROOT / "web/.next/BUILD_ID").read_text().strip(),
                  "schema_ledgers_operations_events_links": counts,
                  "issuer_histories_revisions": issuer_counts,
                  "phases": ["write", "backend_restart", "read_only", "module_disabled"], "locales": ["ru", "en"]}
        (out / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print("Fullstack smoke passed; evidence:", out)
    finally:
        for proc in reversed(processes):
            stop(proc)
        for name in reversed(names):
            run("docker", "rm", "-f", "-v", name, check=False, timeout=15,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for file in files:
            file.close()


def terminate(_signum, _frame):
    raise KeyboardInterrupt


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, terminate)
    main()
