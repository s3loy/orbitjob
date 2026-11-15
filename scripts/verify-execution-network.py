#!/usr/bin/env python3
"""Verify the chart's network policy with real traffic in an owned namespace."""

import argparse
import hashlib
import ipaddress
import json
import subprocess
import sys
import time
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--kubectl", default="kubectl")
    parser.add_argument("--probe-image", default="busybox:1.37")
    args = parser.parse_args()
    namespace = "orbitjob-network-" + uuid.uuid4().hex[:12]
    kubectl = [args.kubectl, "--kubeconfig", args.kubeconfig, "--context", args.context, "--request-timeout=15s"]
    result = {"status": "ERROR", "context": args.context, "namespace": namespace, "checks": []}
    created = False

    def command(argv, body=None, timeout=45):
        completed = subprocess.run(argv, input=body, capture_output=True, text=True, timeout=timeout)
        if completed.returncode:
            raise RuntimeError("command failed: " + " ".join(argv[:8]) + ": " + completed.stderr.strip()[-1200:])
        return completed.stdout

    def k(*argv, body=None, timeout=45):
        return command(kubectl + list(argv), body, timeout)

    def apply(obj):
        k("apply", "-f", "-", body=json.dumps(obj))

    def pod(name, restricted=False, serve=False):
        labels = {"app": "orbitjob-network-probe"}
        if restricted:
            labels.update({"orbitjob.io/execution-network-profile": "restricted-v1", "orbitjob.io/job-run-uid": str(uuid.uuid4())})
        script = "exec sleep 600"
        if serve:
            script = "mkdir -p /tmp/www; echo orbitjob-network-probe > /tmp/www/index.html; httpd -p 8080 -h /tmp/www; httpd -p 8081 -h /tmp/www; exec sleep 600"
        spec = {
            "automountServiceAccountToken": False,
            "serviceAccountName": "network-probe",
            "terminationGracePeriodSeconds": 1,
            "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "seccompProfile": {"type": "RuntimeDefault"}},
            "containers": [{
                "name": "probe", "image": args.probe_image, "command": ["sh", "-c", script],
                "securityContext": {"allowPrivilegeEscalation": False, "capabilities": {"drop": ["ALL"]}},
                "resources": {"requests": {"cpu": "10m", "memory": "8Mi"}, "limits": {"cpu": "100m", "memory": "32Mi"}},
                "readinessProbe": {"exec": {"command": ["sh", "-c", "test -d /tmp/www" if serve else "true"]}, "periodSeconds": 1},
            }],
        }
        return {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": name, "namespace": namespace, "labels": labels}, "spec": spec}

    def install_policy(allowed=None):
        values = {"operator": {"namespaceTenants": namespace + "=00000000000000000000000001"}}
        if allowed:
            values["executionNetwork"] = {"allowedEgress": [{"namespace": namespace, "cidrs": [allowed], "ports": [{"protocol": "TCP", "port": 8080}]}]}
        rendered = command(["helm", "template", "network-probe", "charts/orbitjob", "--namespace", namespace, "--show-only", "templates/execution-network-policy.yaml", "-f", "-"], json.dumps(values))
        k("apply", "-f", "-", body=rendered)
        result.setdefault("policy_sha256", []).append(hashlib.sha256(rendered.encode()).hexdigest())

    def wget(source, address, port):
        # The marker distinguishes an observed connection failure from a failed
        # kubectl exec, a missing executable, or an unreachable API server.
        host = "[" + address + "]" if ":" in address else address
        script = "command -v wget >/dev/null || exit 91; wget -T 2 -qO /tmp/probe-response 'http://" + host + ":" + str(port) + "' 2>/tmp/probe-error; rc=$?; echo ORBITJOB_PROBE_EXIT=$rc; if [ $rc = 0 ]; then cat /tmp/probe-response; else cat /tmp/probe-error; fi"
        output = k("exec", "-n", namespace, source, "--", "sh", "-c", script, timeout=15)
        markers = [line for line in output.splitlines() if line.startswith("ORBITJOB_PROBE_EXIT=")]
        if len(markers) != 1:
            raise RuntimeError("probe process did not report its connection result")
        if markers[0] == "ORBITJOB_PROBE_EXIT=0":
            if "orbitjob-network-probe" not in output:
                raise RuntimeError("probe target returned an unexpected HTTP response")
            return True
        network_errors = ("connection refused", "connection timed out", "download timed out", "network is unreachable", "no route to host", "operation timed out")
        if not any(reason in output.lower() for reason in network_errors):
            raise RuntimeError("probe failed without an observed network connection rejection: " + output[-400:])
        return False

    def check(name, predicate, expected=True):
        deadline = time.monotonic() + 12
        while True:
            observed = predicate()
            if observed == expected:
                result["checks"].append({"name": name, "passed": True})
                return
            if time.monotonic() >= deadline:
                result["checks"].append({"name": name, "passed": False})
                raise AssertionError(name)
            time.sleep(0.5)

    def denied(name, source, target, address, port):
        check(name + "_target_local_before", lambda: wget(target, "127.0.0.1", port))
        check(name, lambda: wget(source, address, port), False)
        check(name + "_target_local_after", lambda: wget(target, "127.0.0.1", port))

    try:
        result["versions"] = json.loads(k("version", "-o", "json"))
        k("create", "namespace", namespace)
        created = True
        apply({"apiVersion": "v1", "kind": "ServiceAccount", "metadata": {"name": "network-probe", "namespace": namespace}, "automountServiceAccountToken": False})
        for obj in [pod("baseline"), pod("target", serve=True), pod("restricted", restricted=True, serve=True)]:
            apply(obj)
        k("wait", "-n", namespace, "--for=condition=Ready", "pods", "--all", "--timeout=90s", timeout=105)
        pods = json.loads(k("get", "pods", "-n", namespace, "-o", "json"))["items"]
        addresses = {p["metadata"]["name"]: p["status"]["podIP"] for p in pods}
        result["images"] = {p["metadata"]["name"]: [s["imageID"] for s in p["status"]["containerStatuses"]] for p in pods}
        check("unrestricted_target_8080", lambda: wget("baseline", addresses["target"], 8080))
        check("unrestricted_target_8081", lambda: wget("baseline", addresses["target"], 8081))
        check("restricted_pod_server_baseline", lambda: wget("baseline", addresses["restricted"], 8080))
        install_policy()
        k("exec", "-n", namespace, "restricted", "--", "nslookup", "kubernetes.default.svc.cluster.local")
        result["checks"].append({"name": "dns_allowed", "passed": True})
        denied("default_egress_denied", "restricted", "target", addresses["target"], 8080)
        denied("default_ingress_denied", "baseline", "restricted", addresses["restricted"], 8080)
        check("denied_target_still_reachable", lambda: wget("baseline", addresses["target"], 8080))
        address = ipaddress.ip_address(addresses["target"])
        install_policy(str(address) + ("/32" if address.version == 4 else "/128"))
        check("explicit_cidr_port_allowed", lambda: wget("restricted", addresses["target"], 8080))
        denied("other_destination_port_denied", "restricted", "target", addresses["target"], 8081)
        check("other_port_target_still_reachable", lambda: wget("baseline", addresses["target"], 8081))
        result["status"] = "PASS"
    except AssertionError as error:
        result.update(status="FAIL", error=str(error))
    except (KeyError, OSError, RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
        result.update(status="ERROR", error=str(error))
    finally:
        if created:
            try:
                k("delete", "pods", "--all", "-n", namespace, "--grace-period=1", "--wait=true", "--timeout=30s", timeout=45)
                k("delete", "namespace", namespace, "--wait=true", "--timeout=90s", timeout=105)
                result["cleanup"] = "complete"
            except (OSError, RuntimeError, subprocess.TimeoutExpired) as error:
                result["cleanup"] = str(error)
                result["status"] = "ERROR"
    print(json.dumps(result, indent=2))
    return 0 if result["status"] == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
