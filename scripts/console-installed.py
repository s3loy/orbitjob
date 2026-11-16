#!/usr/bin/env python3
"""Prepare, run and retire real Console fixtures in one guarded kind installation."""

import argparse
import base64
import contextlib
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
CONTEXT = "kind-orbitjob-console-e2e"
NAMESPACE = "orbitjob"
LABEL = "orbitjob.io/installed-owner"
SPECS = ["console.spec.ts", "m4.spec.ts", "m4-retrospective.spec.ts",
         "m4-unknown.spec.ts", "monitoring.spec.ts", "manifest-export.spec.ts",
         "product.spec.ts", "roadmap.spec.ts", "roadmap-next.spec.ts",
         "documentation.spec.ts"]


def private_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, prefix=path.name, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            json.dump(value, stream, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
            os.replace(temporary, path)
        finally:
            temporary.unlink(missing_ok=True)


def read_private(path):
    info = path.lstat()
    if not path.is_file() or path.is_symlink() or info.st_mode & 0o077:
        raise RuntimeError("State must be a private regular file (0600)")
    return json.loads(path.read_text())


def run(args, env, text=None):
    result = subprocess.run(args, cwd=ROOT, env=env, input=text, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        # Commands may process private credentials; never echo their input or stderr.
        raise RuntimeError(f"{args[0]} {args[1]} failed (exit {result.returncode})")
    return result.stdout


def kube(env, *args, text=None):
    return run(["kubectl", "--context", CONTEXT, "-n", NAMESPACE, *args], env, text)


def guard(kubeconfig):
    path = Path(kubeconfig).expanduser().resolve()
    if not path.is_file() or path == Path.home() / ".kube/config":
        raise RuntimeError("Use a separate kubeconfig file, not ~/.kube/config")
    env = dict(os.environ, KUBECONFIG=str(path), ORBITJOB_CONSOLE_FIXTURE="1")
    if "orbitjob-console-e2e" not in run(["kind", "get", "clusters"], env).splitlines():
        raise RuntimeError("The dedicated local kind cluster is not present")
    config = json.loads(run(["kubectl", "config", "view", "--minify", "--flatten", "--raw", "-o", "json"], env))
    cluster = config["clusters"][0]["cluster"]
    user = config["users"][0]["user"]
    endpoint = urllib.parse.urlparse(cluster["server"])
    if config.get("current-context") != CONTEXT or endpoint.scheme != "https" or endpoint.hostname not in ("127.0.0.1", "localhost", "::1"):
        raise RuntimeError("Only the dedicated loopback kind-orbitjob-console-e2e is allowed")
    if cluster.get("insecure-skip-tls-verify") or not cluster.get("certificate-authority-data") or not user.get("client-certificate-data") or not user.get("client-key-data"):
        raise RuntimeError("The dedicated kubeconfig must verify kind CA and client certificates")
    nodes = json.loads(kube(env, "get", "nodes", "-o", "json"))["items"]
    if not nodes or any(not n["metadata"]["name"].startswith("orbitjob-console-e2e-") for n in nodes):
        raise RuntimeError("Dedicated kind node identity could not be verified")
    cluster_uid = json.loads(kube(env, "get", "namespace", "kube-system", "-o", "json"))["metadata"]["uid"]
    return env, cluster_uid


@contextlib.contextmanager
def forward(env, port, directory):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", port))
        port = listener.getsockname()[1]
    with (directory / "port-forward.log").open("a") as log:
        child = subprocess.Popen(["kubectl", "--context", CONTEXT, "-n", NAMESPACE,
                                  "port-forward", "--address=127.0.0.1",
                                  "service/orbitjob-admin-api", f"{port}:8080"],
                                 env=env, stdout=log, stderr=log)
        try:
            for _ in range(100):
                if child.poll() is not None:
                    raise RuntimeError("Dedicated API port-forward exited")
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                        break
                except OSError:
                    time.sleep(0.2)
            else:
                raise RuntimeError("Dedicated API port-forward did not become ready")
            yield f"http://127.0.0.1:{port}"
        finally:
            child.terminate()
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()


def api(origin, key, path, data=None, method=None, expected=(200, 201)):
    body = None if data is None else json.dumps(data).encode()
    request = urllib.request.Request(origin + "/api/v1/" + path, data=body,
        method=method or ("GET" if data is None else "POST"),
        headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(request, timeout=30) as response:
            status, payload = response.status, response.read()
    except urllib.error.HTTPError as error:
        status, payload = error.code, error.read()
    if status not in expected:
        raise RuntimeError(f"{request.method} {path.split('?')[0]} returned {status}")
    return json.loads(payload) if payload else None


def definition_inventory(origin, key, collection):
    rows, seen = [], set()
    for page in range(1000):
        items = api(origin, key, f"{collection}?limit=100&offset={page * 100}")["items"]
        for row in items:
            identifier = str(row["id"])
            if identifier in seen:
                raise RuntimeError("Definition pagination repeated an ID; cleanup was not accepted")
            seen.add(identifier)
            rows.append(row)
        if len(items) < 100:
            return rows
    raise RuntimeError("Definition inventory exceeded its cleanup bound; credentials remain active")


def wait_for(description, probe, timeout=180):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        result = probe()
        if result:
            return result
        time.sleep(1)
    raise RuntimeError("Timed out waiting for " + description)


def helper(env, command, manifest, *extra):
    output = run(["go", "run", "./deploy/kind/console-fixture", command,
                  "--manifest", str(manifest), *extra], env)
    if output.strip():
        print(output.strip(), flush=True)


def load_images(env):
    images = set()
    for filename in ("console-jobs.json", "console-workflows.json"):
        items = json.loads((ROOT / "deploy/kind/fixtures" / filename).read_text())["items"]
        images.update(item["spec"]["jobTemplate"]["image"] for item in items if item["kind"] == "ScheduledJob")
    nodes = json.loads(kube(env, "get", "nodes", "-o", "json"))["items"]
    architectures = {node["status"]["nodeInfo"]["architecture"] for node in nodes}
    if not nodes or len(architectures) != 1 or not architectures <= {"amd64", "arm64"}:
        raise RuntimeError("Console fixtures require one supported architecture across dedicated kind nodes")
    platform = "linux/" + architectures.pop()
    for image in sorted(images):
        print("Loading fixture image on every dedicated node: " + image, flush=True)
        run(["docker", "pull", "--platform", platform, image], env)
        with tempfile.TemporaryDirectory(prefix="console-image-") as folder:
            archive = Path(folder) / "image.tar"
            run(["docker", "image", "save", "-o", str(archive), image], env)
            for node in nodes:
                with archive.open("rb") as stream:
                    result = subprocess.run(["docker", "exec", "--privileged", "-i", node["metadata"]["name"],
                        "ctr", "--namespace=k8s.io", "images", "import", "--platform", platform, "--digests", "-"],
                        env=env, stdin=stream, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                if result.returncode:
                    raise RuntimeError("Fixture image import failed on a dedicated kind node")
                run(["docker", "exec", node["metadata"]["name"], "crictl", "inspecti", image], env)
    probe_images = re.findall(r'^const ProbeImage = "([^"]+)"$',
        (ROOT / "internal/core/app/projection/check.go").read_text(), re.MULTILINE)
    if len(probe_images) != 1 or not re.fullmatch(r"curlimages/curl@sha256:[0-9a-f]{64}", probe_images[0]):
        raise RuntimeError("Cannot resolve the Check probe's pinned image from its owning source")
    probe_image = probe_images[0]
    for node in nodes:
        name = node["metadata"]["name"]
        print("Preparing Check probe image on dedicated node: " + name, flush=True)
        # Pull before timed Check execution; missing images must fail preparation.
        cached = subprocess.run(["docker", "exec", name, "crictl", "inspecti", probe_image],
                                env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if cached.returncode:
            run(["docker", "exec", name, "crictl", "pull", "--pull-timeout=300s", probe_image], env)
        observed = json.loads(run(["docker", "exec", name, "crictl", "inspecti", probe_image], env))
        if observed["info"]["imageSpec"]["architecture"] != platform.split("/")[1]:
            raise RuntimeError("Check probe image architecture differs from the dedicated nodes")
        if "docker.io/" + probe_image not in observed["status"].get("repoDigests", []):
            raise RuntimeError("Check probe image does not retain its pinned published digest")


def apply_fixtures(env, state, directory):
    owned = []
    for filename in ("console-jobs.json", "console-workflows.json"):
        listing = json.loads((ROOT / "deploy/kind/fixtures" / filename).read_text())
        for item in listing["items"]:
            metadata = item["metadata"]
            current = kube(env, "get", item["kind"], metadata["name"], "--ignore-not-found", "-o", "json")
            if current:
                raise RuntimeError("Fixture name already exists; prepare requires a fresh installation")
            metadata.setdefault("labels", {})[LABEL] = state["prefix"]
            result = json.loads(kube(env, "create", "-f", "-", "-o", "json", text=json.dumps(item)))
            owned.append({"kind": item["kind"], "name": metadata["name"], "uid": result["metadata"]["uid"]})
            private_json(directory / "owned-resources.json", owned)


def history(origin, state, directory, env):
    key = state["key"]
    def definition(kind, name):
        return wait_for(name, lambda: next((row for row in api(origin, key, kind + "?limit=100")["items"] if row["name"] == name), None))
    job = definition("jobs", "console-success")
    for _ in range(4):
        receipt = api(origin, key, f"jobs/{job['id']}/trigger", {})
        query = urllib.parse.urlencode({"occurrence_key": receipt["occurrence_key"]})
        wait_for("real Job history", lambda: any(row["phase"] == "Succeeded" for row in api(origin, key, "instances?" + query)["items"]))
    functions = read_private(directory / "m4-functions.json")["functions"]
    success = next(row for row in functions if row["case"] == "success")
    api(origin, key, f"functions/{success['id']}/invoke", method="POST")
    wait_for("real Function history", lambda: any(row["status"] == "success" for row in api(origin, key, f"functions/{success['id']}/runs")["items"]))
    workflow = definition("workflows", "console-m4-dag")
    receipt = api(origin, key, f"workflows/{workflow['id']}/runs", {})
    wait_for("real Workflow history", lambda: any(row["occurrence_key"] == receipt["occurrence_key"] and row["phase"] == "Failed" for row in api(origin, key, f"workflows/{workflow['id']}/runs")["items"]))
    # Give this existing UID a new immutable revision after its first execution.
    current = json.loads(kube(env, "get", "workflowjob", "console-m4-dag", "-o", "json"))
    kube(env, "patch", "workflowjob", "console-m4-dag", "--type=merge", "-p",
         json.dumps({"metadata": {"resourceVersion": current["metadata"]["resourceVersion"]}, "spec": {"timeoutSeconds": current["spec"]["timeoutSeconds"] + 1}}))
    wait_for("new Workflow revision", lambda: str(definition("workflows", "console-m4-dag")["id"]) != str(workflow["id"]))


def prepare(env, origin, directory, cluster_uid):
    credentials = directory / "credentials.json"
    if credentials.exists():
        raise RuntimeError("Existing credentials retained; use run/cleanup or a fresh state directory")
    load_images(env)
    secret = json.loads(kube(env, "get", "secret", "bootstrap-api-key", "-o", "json"))
    bootstrap = base64.b64decode(secret["data"]["api-key"]).decode()
    tenant = api(origin, bootstrap, "identity")["tenant_id"]
    try:
        helper(env, "credentials", credentials, "--tenant", tenant)
    finally:
        if credentials.exists():
            state = read_private(credentials)
            state["clusterUID"] = cluster_uid
            private_json(credentials, state)
    state = read_private(credentials)
    apply_fixtures(env, state, directory)
    helper(env, "seed", directory / "m4-functions.json", "--tenant", tenant,
           "--foreign-tenant", state["foreignTenantId"], "--group", state["groupId"], "--timeout", "5m")
    history(origin, state, directory, env)
    state["prepared"] = True
    state["preparedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    private_json(credentials, state)
    print("Prepared real Console fixtures; credentials retained privately at " + str(credentials))


def source_manifest():
    paths = [ROOT / "scripts/console-installed.py", ROOT / "web/package.json",
             ROOT / "web/package-lock.json", ROOT / "web/installed.config.ts"]
    for parent in ("web/src", "web/installed-tests", "deploy/kind/console-fixture", "deploy/kind/fixtures"):
        paths.extend(p for p in (ROOT / parent).rglob("*") if p.is_file())
    return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths)}


def report_tests(suites):
    for suite in suites:
        for spec in suite.get("specs", []):
            yield from spec.get("tests", [])
        yield from report_tests(suite.get("suites", []))


def report_passed(exit_code, report):
    tests = list(report_tests(report.get("suites", [])))
    return exit_code == 0 and bool(tests) and not report.get("errors") and all(
        test.get("expectedStatus") == "passed" and len(test.get("results", [])) == 1
        and test["results"][0].get("status") == "passed" for test in tests)


def run_suite(env, origin, directory, state, specs):
    if not state.get("prepared") or state.get("retired"):
        raise RuntimeError("Prepare must complete before running the suite")
    source = source_manifest()
    run_directory = directory / "runs" / datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
    run_directory.mkdir(parents=True, mode=0o700)
    observed = json.loads(kube(env, "get", "pods", "-o", "json"))
    images = [{"pod": p["metadata"]["name"], "images": [c.get("imageID") for c in p.get("status", {}).get("containerStatuses", [])]}
              for p in observed["items"] if p["metadata"]["name"].startswith("orbitjob-")]
    receipt = {"context": CONTEXT, "clusterUID": state["clusterUID"], "source": source,
               "images": images, "specs": [], "completeSuite": specs == SPECS, "passed": False}
    receipt_path = run_directory / "verification.json"
    def record():
        private_json(receipt_path, receipt)
        private_json(directory / "verification.json", receipt)
    record()
    failed_specs = []
    for spec in specs:
        output = run_directory / "results" / spec.removesuffix(".spec.ts")
        output.mkdir(parents=True, exist_ok=True, mode=0o700)
        report = output / "report.json"
        report.unlink(missing_ok=True)
        selected_env = dict(env, ORBITJOB_CONSOLE_CREDENTIALS=str(directory / "credentials.json"),
                            ORBITJOB_CONSOLE_URL=origin, ORBITJOB_CONSOLE_REPORT_DIR=str(output),
                            ORBITJOB_M3_EVIDENCE=str(directory / "monitoring-evidence.json"))
        print("Running " + spec, flush=True)
        # Playwright clears its outputDir when starting; keep the process log outside it.
        with output.with_suffix(".log").open("w") as log:
            result = subprocess.run(["npm", "run", "test:installed", "--", spec], cwd=ROOT / "web",
                                    env=selected_env, stdout=log, stderr=subprocess.STDOUT)
        parsed = json.loads(report.read_text()) if report.exists() else {}
        tests = list(report_tests(parsed.get("suites", [])))
        passed = report_passed(result.returncode, parsed)
        receipt["specs"].append({"file": spec, "exitCode": result.returncode,
                                 "tests": len(tests), "passed": passed, "stats": parsed.get("stats")})
        record()
        if not passed:
            failed_specs.append(spec)
    if source_manifest() != source:
        raise RuntimeError("Console verification inputs changed during the run")
    if failed_specs:
        raise RuntimeError("Installed Console checks did not pass every test without skips: "
                           + ", ".join(failed_specs) + "; inspect " + str(run_directory))
    receipt["passed"] = True
    record()
    print("Installed Console checks passed without skipped tests; receipt: " + str(receipt_path))


def cleanup(env, origin, directory, state):
    if state.get("retired"):
        print("Fixture credentials were already retired; historical evidence retained.")
        return
    if not state.get("authPrepared"):
        helper(env, "revoke-credentials", directory / "credentials.json")
        print("Incomplete credential transaction retired; no workload fixtures were prepared.")
        return
    key, prefix = state["key"], state["prefix"]
    # Stop recurring work created through the UI; retain its ledger and audit evidence.
    inventory = {collection: definition_inventory(origin, key, collection)
                 for collection in ("checks", "slos", "functions")}
    for collection in ("checks", "slos"):
        for row in inventory[collection]:
            if row["name"].startswith(prefix) and row["status"] != "paused":
                api(origin, key, f"{collection}/{row['id']}/pause", {"version": row["version"]})
    for row in inventory["functions"]:
        if row["name"].startswith(prefix) and row["status"] != "paused":
            detail = api(origin, key, f"functions/{row['id']}")
            fields = ("name", "description", "image", "command", "args", "timeout_seconds", "retry_limit", "history_success", "history_failed", "labels")
            body = {field: detail[field] for field in fields if field in detail}
            body.update(status="paused", expected_version=detail["version"])
            api(origin, key, f"functions/{row['id']}", body, method="PUT")
    manifest = directory / "m4-functions.json"
    if manifest.exists() and not read_private(manifest).get("cleaned"):
        helper(env, "cleanup", manifest, "--timeout", "5m")
    owned_path = directory / "owned-resources.json"
    owned = read_private(owned_path) if owned_path.exists() else []
    for resource in ("scheduledjobs", "workflowjobs"):
        for item in json.loads(kube(env, "get", resource, "-o", "json"))["items"]:
            metadata = item["metadata"]
            if metadata["name"].startswith(prefix) or metadata.get("labels", {}).get(LABEL) == prefix:
                owned.append({"kind": resource, "name": metadata["name"], "uid": metadata["uid"]})
    for item in owned:
        raw = kube(env, "get", item["kind"], item["name"], "--ignore-not-found", "-o", "json")
        if not raw:
            continue
        actual = json.loads(raw)["metadata"]
        if actual["uid"] != item["uid"]:
            raise RuntimeError("Cleanup refused a recreated fixture resource")
        plural = "scheduledjobs" if item["kind"].lower() in ("scheduledjob", "scheduledjobs") else "workflowjobs"
        path = f"/apis/workloads.orbitjob.io/v1alpha1/namespaces/{NAMESPACE}/{plural}/{item['name']}"
        options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions":
                   {"uid": item["uid"], "resourceVersion": actual["resourceVersion"]}}
        kube(env, "delete", "--raw", path, "-f", "-", text=json.dumps(options))
    # Recorded keys use the public audited route first. The guarded owner helper
    # discovers interrupted writes through their transactional creation audits.
    latest = read_private(directory / "credentials.json")
    for identifier in latest.get("extraTenantKeyIDs", []):
        api(origin, key, f"api_keys/{identifier}/revoke", {}, expected=(200, 404))
    helper(env, "revoke-credentials", directory / "credentials.json")
    print("Fixture definitions retired and credentials revoked; cluster and historical evidence retained.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "run", "cleanup"))
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--state-dir", default=str(ROOT / ".runtime/console-installed"))
    parser.add_argument("--port", type=int, default=18081)
    parser.add_argument("--spec", action="append", choices=SPECS)
    options = parser.parse_args()
    os.umask(0o077)
    if not 0 <= options.port <= 65535:
        parser.error("--port must be 0..65535")
    directory = Path(options.state_dir).expanduser().resolve()
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    if directory.stat().st_mode & 0o077:
        raise RuntimeError("State directory must be private (0700)")
    if options.command == "cleanup" and not (directory / "credentials.json").exists():
        print("No credential manifest was created; there are no prepared fixtures to retire.")
        return
    env, cluster_uid = guard(options.kubeconfig)
    lock = directory / "harness.lock"
    descriptor = os.open(lock, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.close(descriptor)
    try:
        state = None
        if options.command != "prepare":
            state = read_private(directory / "credentials.json")
            if state.get("clusterUID") != cluster_uid:
                raise RuntimeError("State belongs to a different cluster instance")
        with forward(env, options.port, directory) as origin:
            if options.command == "prepare":
                prepare(env, origin, directory, cluster_uid)
            elif options.command == "run":
                run_suite(env, origin, directory, state, options.spec or SPECS)
            else:
                cleanup(env, origin, directory, state)
    finally:
        lock.unlink()


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError, KeyError) as error:
        print("console-installed: " + str(error), file=sys.stderr)
        sys.exit(1)
