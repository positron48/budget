#!/usr/bin/env python3
"""Build and exercise the exact application Dockerfiles on a disposable network.

Uses the authenticated financial browser scenarios; accepts no DB, credentials,
registry push or deployment target. All server containers belong to this run.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import struct
import urllib.request
import subprocess
import tempfile
import time
import uuid

from fullstack_smoke import ROOT, run, terminate, wait_tcp


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", help="new private evidence directory")
    parser.add_argument("--backend-platform", choices=["linux/amd64", "linux/arm64"], help="build backend for this platform; requires Docker emulation when non-native")
    parser.add_argument("--backend-image", help="existing local image; skips backend build")
    parser.add_argument("--web-image", help="existing local image; skips web build (must embed budget-smoke-envoy rewrite)")
    parser.add_argument("--host-build-dns", action="store_true", help="resolve known public dependency hosts on the host; applies only to build RUN steps")
    args = parser.parse_args()
    if args.backend_image and args.backend_platform:
        parser.error("--backend-platform cannot accompany an existing --backend-image")
    out = Path(args.out).resolve() if args.out else Path(tempfile.mkdtemp(prefix="budget-container-smoke-"))
    if args.out:
        out.mkdir(mode=0o700)
    os.chmod(out, 0o700)
    with socket.socket() as sock:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(("127.0.0.1", 3030))
    run_id = uuid.uuid4().hex
    prefix = "budget-container-smoke-" + run_id[:12]
    network = prefix + "-net"
    front_network = prefix + "-front"
    names, files, built_images = [], [], []
    networks_created = []
    build_dns = {}
    build_options = []
    if args.host_build_dns:
        for host in ["proxy.golang.org", "sum.golang.org", "storage.googleapis.com", "dl-cdn.alpinelinux.org", "registry.npmjs.org", "fonts.googleapis.com", "fonts.gstatic.com"]:
            build_dns[host] = socket.gethostbyname(host)
            build_options += ["--add-host", host + ":" + build_dns[host]]

    def log(name):
        handle = (out / name).open("w")
        files.append(handle)
        return handle

    def docker(*args, **kwargs):
        return run("docker", *args, **kwargs)

    def start(label, image, *options, network_name=None):
        name = prefix + "-" + label
        names.append(name)
        security = ["--cap-drop=ALL", "--security-opt=no-new-privileges"] if label.startswith(("backend-", "web-")) else []
        docker("run", "-d", "--name", name, "--network", network_name or network, *security, *options, image,
               stdout=subprocess.DEVNULL)
        return name

    def capture(name, label):
        docker("logs", name, check=False, timeout=15, stdout=log(label + ".log"), stderr=subprocess.STDOUT)

    try:
        backend_image = args.backend_image or prefix + ":backend"
        web_image = args.web_image or prefix + ":web"
        if not args.backend_image:
            built_images.append(backend_image)
            docker("buildx", "build", "--load", *build_options, *(["--platform", args.backend_platform] if args.backend_platform else []), "--progress=plain", "-f", "deploy/Dockerfile", "-t", backend_image, ".",
                   cwd=ROOT, timeout=1200, stdout=log("backend-build.log"), stderr=subprocess.STDOUT)
            if "InvalidBaseImagePlatform" in (out / "backend-build.log").read_text():
                raise RuntimeError("base image platform mismatch; refresh the official target-platform base image")
        if not args.web_image:
            built_images.append(web_image)
            docker("buildx", "build", "--load", *build_options, "--progress=plain", "-f", "web/Dockerfile", "-t", web_image,
                   "--build-arg", "NEXT_PUBLIC_GRPC_BASE_URL=http://127.0.0.1:3030/grpc",
                   "--build-arg", "NEXT_PUBLIC_APP_URL=http://127.0.0.1:3030",
                   "--build-arg", "ENVOY_URL=http://budget-smoke-envoy:8081", "web",
                   cwd=ROOT, timeout=1200, stdout=log("web-build.log"), stderr=subprocess.STDOUT)
            if "InvalidBaseImagePlatform" in (out / "web-build.log").read_text():
                raise RuntimeError("web base image platform mismatch; refresh the official native base image")
        # Pin local immutable IDs before starting: changing a tag cannot replace
        # the image between write/restart/read-only phases.
        identities = {}
        for label, image in [("backend", backend_image), ("web", web_image)]:
            info = json.loads(docker("image", "inspect", image, capture_output=True).stdout)[0]
            if not info["Config"].get("User") or info["Config"]["User"] in ["0", "root", "0:0"]:
                raise RuntimeError("application image must declare a nonroot user")
            identities[label] = {"id": info["Id"], "os": info["Os"], "architecture": info["Architecture"],
                                 "configured_user": info["Config"]["User"],
                                 "built_here": not bool(args.backend_image if label == "backend" else args.web_image)}
        backend_id, web_id = identities["backend"]["id"], identities["web"]["id"]
        binaries = {}
        audit_name = prefix + "-image-audit"
        names.append(audit_name)
        docker("create", "--name", audit_name, backend_id, stdout=subprocess.DEVNULL)
        for command in ["budgetd", "marketdata", "investmentnav", "investcbr", "investfxpolicy", "investalphafx", "alphafx"]:
            path = out / (command + ".elf")
            docker("cp", audit_name + ":/app/" + command, str(path))
            header = path.read_bytes()[:20]
            if len(header) != 20 or header[:6] != b"\x7fELF\x02\x01":
                raise RuntimeError("packaged binary must be a little-endian ELF64")
            architecture = {62: "amd64", 183: "arm64"}.get(struct.unpack("<H", header[18:20])[0])
            if architecture != identities["backend"]["architecture"]:
                raise RuntimeError("packaged ELF architecture differs from its image platform")
            binaries[command] = {"architecture": architecture, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            path.unlink()
        docker("rm", audit_name, stdout=subprocess.DEVNULL)
        docker("network", "create", "--internal", network, stdout=subprocess.DEVNULL)
        networks_created.append(network)
        docker("network", "create", front_network, stdout=subprocess.DEVNULL)
        networks_created.append(front_network)
        pg = start("pg", "postgres:16", "--network-alias", "budget-smoke-pg", "-e", "POSTGRES_USER=budget",
                   "-e", "POSTGRES_PASSWORD=smoke-disposable", "-e", "POSTGRES_DB=budget")
        start("redis", "redis:7-alpine", "--network-alias", "budget-smoke-redis")
        deadline = time.monotonic() + 45
        while docker("exec", pg, "pg_isready", "-U", "budget", check=False,
                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
            if time.monotonic() >= deadline:
                raise RuntimeError("disposable PostgreSQL readiness timeout")
            time.sleep(0.2)
        backend_env = dict(APP_ENV="test", GRPC_ADDR="0.0.0.0:8080",
                           DATABASE_URL="postgres://budget:smoke-disposable@budget-smoke-pg:5432/budget?sslmode=disable",
                           REDIS_URL="redis://budget-smoke-redis:6379", JWT_SIGN_KEY=uuid.uuid4().hex,
                           JWT_ACCESS_TTL="1h", JWT_REFRESH_TTL="24h", AUTH_PASSWORD_ENABLED="true",
                           FX_IMPORT_ENABLED="false", METRICS_ADDR="", OTEL_EXPORTER_OTLP_ENDPOINT="",
                           INVESTMENTS_TENANT_ALLOWLIST="", GOOGLE_CLIENT_ID="",
                           OAUTH_AUTH_TOKEN_TTL="5m", OAUTH_SESSION_TTL="1h", OAUTH_VERIFICATION_CODE_TTL="5m",
                           OAUTH_MAX_ATTEMPTS_PER_HOUR="30", OAUTH_MAX_ATTEMPTS_PER_10MIN="10",
                           OAUTH_WEB_BASE_URL="http://127.0.0.1:3030")

        def backend(phase):
            variables = dict(backend_env, INVESTMENTS_ENABLED="false" if phase == "disabled" else "true",
                             INVESTMENTS_READ_ONLY="true" if phase == "readonly" else "false")
            options = ["--network-alias", "budget-smoke-backend", "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m"]
            for key, value in variables.items():
                options += ["-e", key + "=" + value]
            name = start("backend-" + phase, backend_id, *options)
            deadline = time.monotonic() + 45
            while "gRPC listening" not in docker("logs", name, capture_output=True).stderr:
                state = docker("inspect", "--format", "{{.State.Running}}", name, capture_output=True).stdout.strip()
                if state != "true" or time.monotonic() >= deadline:
                    raise RuntimeError("owned backend did not reach gRPC readiness; inspect container log")
                time.sleep(0.2)
            return name

        def web(phase):
            name = start("web-" + phase, web_id, "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m",
                         "--tmpfs", "/app/.next/cache:rw,noexec,nosuid,size=32m,uid=1001,gid=1001",
                         "-p", "127.0.0.1:3030:3030", "-e", "PORT=3030", "-e", "HOSTNAME=0.0.0.0",
                         "-e", "INVESTMENTS_ENABLED=" + ("false" if phase == "disabled" else "true"),
                         "-e", "INVESTMENTS_READ_ONLY=" + ("true" if phase == "readonly" else "false"), network_name=front_network)
            docker("network", "connect", network, name)
            return name

        backend_name = backend("write")
        envoy_name = prefix + "-envoy"
        names.append(envoy_name)
        config = (ROOT / "deploy/envoy.yaml").read_text()
        config = config.replace("type: static", "type: STRICT_DNS\n    dns_lookup_family: V4_ONLY")
        config = config.replace("address: 127.0.0.1, port_value: 8080", "address: budget-smoke-backend, port_value: 8080")
        config = config.replace("address: 127.0.0.1", "address: 0.0.0.0")
        (out / "envoy.yaml").write_text(config)
        docker("create", "--name", envoy_name, "--network", network, "--network-alias", "budget-smoke-envoy",
               "--user", "65532:65532", "--entrypoint", "envoy",
               "--cap-drop=ALL", "--security-opt=no-new-privileges", "envoyproxy/envoy:v1.36.7",
               "-c", "/tmp/smoke-envoy.yaml", "--log-level", "warning", stdout=subprocess.DEVNULL)
        docker("cp", str(out / "envoy.yaml"), envoy_name + ":/tmp/smoke-envoy.yaml")
        docker("start", envoy_name, stdout=subprocess.DEVNULL)
        web_name = web("write")
        wait_tcp(3030)
        docker("cp", web_name + ":/app/.next/routes-manifest.json", str(out / "routes-manifest.json"))
        manifest = json.loads((out / "routes-manifest.json").read_text())
        rewrites = manifest.get("rewrites", {})
        rules = rewrites if isinstance(rewrites, list) else sum(rewrites.values(), [])
        grpc = [r for r in rules if r.get("source") == "/grpc/:path*"]
        if len(grpc) != 1 or grpc[0].get("destination") != "http://budget-smoke-envoy:8081/:path*":
            raise RuntimeError("web image must embed a rewrite to this isolated network's Envoy")
        docker("cp", web_name + ":/app/.next/BUILD_ID", str(out / "BUILD_ID"))
        browser_env = dict(os.environ, E2E_FULLSTACK="1", E2E_BASE_URL="http://127.0.0.1:3030", E2E_FULLSTACK_RUN=run_id)
        for phase in ["write", "restart", "readonly", "disabled"]:
            if phase == "restart":
                docker("restart", backend_name, stdout=subprocess.DEVNULL)
            elif phase in ["readonly", "disabled"]:
                capture(backend_name, "backend-before-" + phase)
                capture(web_name, "web-before-" + phase)
                docker("rm", "-f", backend_name, web_name, stdout=subprocess.DEVNULL)
                backend_name, web_name = backend(phase), web(phase)
                docker("restart", envoy_name, stdout=subprocess.DEVNULL)
                wait_tcp(3030)
            deadline = time.monotonic() + 45
            while True:
                try:
                    with urllib.request.urlopen("http://127.0.0.1:3030/login", timeout=1):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("container frontend readiness timeout")
                    time.sleep(0.2)
            run("npx", "playwright", "test", "e2e/investment-fullstack.spec.ts", "--workers=1", "--reporter=line",
                cwd=ROOT / "web", env=dict(browser_env, E2E_FULLSTACK_PHASE=phase),
                stdout=log("browser-" + phase + ".log"), stderr=subprocess.STDOUT)
        query = "SELECT (SELECT version FROM schema_migrations), (SELECT count(*) FROM investment_ledgers), (SELECT count(*) FROM investment_operations), (SELECT count(*) FROM investment_payout_events), (SELECT count(*) FROM investment_payout_links);"
        counts = docker("exec", pg, "psql", "-U", "budget", "-d", "budget", "-At", "-c", query, capture_output=True).stdout.strip()
        if counts.split("|")[1:] != ["2", "6", "0", "0"]:
            raise RuntimeError("unexpected financial counts after container restart and flags")
        workers = {}
        for command in ["marketdata", "investmentnav", "investcbr", "investfxpolicy", "investalphafx", "alphafx"]:
            result = docker("run", "--rm", "--network=none", "--read-only", "--cap-drop=ALL",
                            "--security-opt=no-new-privileges", "--entrypoint", "/app/" + command,
                            backend_id, "-h", check=False, stdout=log(command + "-help.log"), stderr=subprocess.STDOUT)
            if result.returncode != 0:
                raise RuntimeError("packaged worker failed help startup")
            workers[command] = "help-startup-passed"
        report = {"status": "passed", "scope": "disposable container images; synthetic authenticated data; no production",
                  "images": identities, "packaged_binaries": binaries, "workers": workers, "temporary_build_dns": build_dns, "web_build_id": (out / "BUILD_ID").read_text().strip(),
                  "dockerfile_sha256": {p: hashlib.sha256((ROOT / p).read_bytes()).hexdigest() for p in ["deploy/Dockerfile", "web/Dockerfile"]},
                  "schema_ledgers_operations_events_links": counts,
                  "phases": ["write", "actual_backend_container_restart", "read_only", "module_disabled"], "locales": ["ru", "en"]}
        (out / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print("Container smoke passed; evidence:", out)
    finally:
        cleanup_failures = []
        for name in reversed(names):
            try:
                capture(name, name.removeprefix(prefix + "-") + "-final")
            except (OSError, subprocess.SubprocessError):
                cleanup_failures.append("logs")
            try:
                docker("rm", "-f", "-v", name, check=False, timeout=15,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            except (OSError, subprocess.SubprocessError):
                cleanup_failures.append("container")
        for owned_network in reversed(networks_created):
            try:
                docker("network", "rm", owned_network, check=False, timeout=15, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            except (OSError, subprocess.SubprocessError):
                cleanup_failures.append("network")
        for image in built_images:
            try:
                docker("image", "rm", image, check=False, timeout=15, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            except (OSError, subprocess.SubprocessError):
                cleanup_failures.append("image")
        for handle in files:
            handle.close()
        if cleanup_failures:
            raise RuntimeError("owned resource cleanup or diagnostics failed; inspect Docker state")


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, terminate)
    main()
