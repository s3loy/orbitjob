"""Safety and result-contract tests; never connect to a cluster."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("console_installed", Path(__file__).with_name("console-installed.py"))
harness = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(harness)


class HarnessTest(unittest.TestCase):
    def test_cleanup_retires_owned_definitions_after_the_first_page(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)
            state = {"key": "fixture-key", "prefix": "console-installed-123", "authPrepared": True}
            harness.private_json(directory / "credentials.json", state)
            calls = []

            def api(origin, key, path, data=None, method=None, **kwargs):
                calls.append((path, data, method))
                if "?" in path:
                    collection, query = path.split("?")
                    offset = int(dict(part.split("=") for part in query.split("&"))["offset"])
                    if offset == 0:
                        return {"items": [{"id": n, "name": "unrelated", "status": "active"}
                                          for n in range(100)]}
                    self.assertEqual(offset, 100)
                    return {"items": [{"id": 101, "name": state["prefix"] + "-" + collection,
                                       "status": "active", "version": 4}]}
                if path == "functions/101" and data is None:
                    return {"name": state["prefix"] + "-functions", "image": "fixture:1", "version": 4}
                return {}

            with mock.patch.object(harness, "api", side_effect=api), \
                    mock.patch.object(harness, "kube", return_value='{"items":[]}'), \
                    mock.patch.object(harness, "helper") as helper:
                harness.cleanup({}, "http://fixture", directory, state)
            helper.assert_called_once_with({}, "revoke-credentials", directory / "credentials.json")
            writes = [(path, data, method) for path, data, method in calls if data is not None]
            self.assertEqual(writes, [("checks/101/pause", {"version": 4}, None),
                                      ("slos/101/pause", {"version": 4}, None),
                                      ("functions/101", {"name": state["prefix"] + "-functions",
                                                         "image": "fixture:1", "status": "paused",
                                                         "expected_version": 4}, "PUT")])

    def test_incomplete_definition_inventory_never_retires_credentials(self):
        for failure in ("request", "repeated-page"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as folder:
                directory = Path(folder)
                state = {"key": "fixture-key", "prefix": "console-installed-123", "authPrepared": True}
                harness.private_json(directory / "credentials.json", state)
                first = {"items": [{"id": n, "name": state["prefix"], "status": "active", "version": 1}
                                   for n in range(100)]}
                second = RuntimeError("page failed") if failure == "request" else first
                with mock.patch.object(harness, "api", side_effect=[first, second]) as api, \
                        mock.patch.object(harness, "kube") as kube, \
                        mock.patch.object(harness, "helper") as helper:
                    with self.assertRaises(RuntimeError):
                        harness.cleanup({}, "http://fixture", directory, state)
                self.assertEqual(api.call_count, 2)
                helper.assert_not_called()
                kube.assert_not_called()
                self.assertFalse(harness.read_private(directory / "credentials.json").get("retired", False))

    def test_atomic_credentials_are_private_and_do_not_follow_old_temp_symlink(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "credentials.json"
            victim = Path(folder) / "unrelated"
            victim.write_text("retain")
            path.with_suffix(".json.tmp").symlink_to(victim)
            harness.private_json(path, {"prepared": False})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(harness.read_private(path), {"prepared": False})
            self.assertEqual(victim.read_text(), "retain")
            path.chmod(0o644)
            with self.assertRaisesRegex(RuntimeError, "private regular"):
                harness.read_private(path)

    def test_reports_refuse_missing_skipped_expected_failure_and_retried_tests(self):
        def report(expected="passed", statuses=("passed",)):
            return {"suites": [{"suites": [{"specs": [{"tests": [{
                "expectedStatus": expected, "results": [{"status": s} for s in statuses]
            }]}]}]}]}
        self.assertTrue(harness.report_passed(0, report()))
        self.assertFalse(harness.report_passed(1, report()))
        self.assertFalse(harness.report_passed(0, {}))
        self.assertFalse(harness.report_passed(0, report("skipped", ("skipped",))))
        self.assertFalse(harness.report_passed(0, report("failed", ("failed",))))
        self.assertFalse(harness.report_passed(0, report(statuses=("failed", "passed"))))
        self.assertFalse(harness.report_passed(0, dict(report(), errors=[{"message": "global failure"}])))

    def test_guard_refuses_remote_unverified_and_wrong_cluster_before_kube_reads(self):
        with tempfile.TemporaryDirectory() as folder:
            config_path = Path(folder) / "kubeconfig"
            config_path.write_text("fixture")
            valid = {"current-context": harness.CONTEXT,
                     "clusters": [{"cluster": {"server": "https://127.0.0.1:1234", "certificate-authority-data": "ca"}}],
                     "users": [{"user": {"client-certificate-data": "cert", "client-key-data": "key"}}]}
            for field, value in (("server", "https://production.example"), ("insecure-skip-tls-verify", True), ("certificate-authority-data", "")):
                bad = json.loads(json.dumps(valid))
                bad["clusters"][0]["cluster"][field] = value
                with mock.patch.object(harness, "run", side_effect=["orbitjob-console-e2e\n", json.dumps(bad)]), mock.patch.object(harness, "kube") as kube:
                    with self.assertRaises(RuntimeError):
                        harness.guard(config_path)
                    kube.assert_not_called()
            with mock.patch.object(harness, "run", return_value="orbitjob-dev\n"), mock.patch.object(harness, "kube") as kube:
                with self.assertRaises(RuntimeError):
                    harness.guard(config_path)
                kube.assert_not_called()

    def test_cleanup_without_credentials_never_connects(self):
        with tempfile.TemporaryDirectory() as folder, mock.patch.object(harness, "guard") as guard:
            with mock.patch("sys.argv", ["console-installed.py", "cleanup", "--kubeconfig", "/unavailable", "--state-dir", folder]):
                harness.main()
            guard.assert_not_called()

    def test_suite_records_later_specs_after_a_failed_report_and_still_fails(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)
            specs = ["first.spec.ts", "second.spec.ts"]
            source = {"web/src/console.ts": "source-digest"}
            observed = {"items": [{"metadata": {"name": "orbitjob-api"}, "status": {
                "containerStatuses": [{"imageID": "sha256:installed-image"}]}}]}

            def execute(args, **kwargs):
                passed = args[-1] == specs[1]
                report = {"suites": [{"specs": [{"tests": [{"expectedStatus": "passed",
                    "results": [{"status": "passed" if passed else "failed"}]}]}]}]}
                output = Path(kwargs["env"]["ORBITJOB_CONSOLE_REPORT_DIR"])
                (output / "report.json").write_text(json.dumps(report))
                return SimpleNamespace(returncode=0 if passed else 1)

            with mock.patch.object(harness, "source_manifest", return_value=source), \
                    mock.patch.object(harness, "kube", return_value=json.dumps(observed)), \
                    mock.patch.object(harness.subprocess, "run", side_effect=execute) as execute_mock:
                with self.assertRaisesRegex(RuntimeError, "first.spec.ts"):
                    harness.run_suite({}, "http://127.0.0.1:8080", directory,
                                      {"prepared": True, "clusterUID": "cluster-uid"}, specs)

            self.assertEqual([call.args[0][-1] for call in execute_mock.call_args_list], specs)
            receipts = [directory / "verification.json", *directory.glob("runs/*/verification.json")]
            self.assertEqual(len(receipts), 2)
            for path in receipts:
                receipt = harness.read_private(path)
                self.assertFalse(receipt["passed"])
                self.assertFalse(receipt["completeSuite"])
                self.assertEqual(receipt["source"], source)
                self.assertEqual(receipt["images"], [{"pod": "orbitjob-api", "images": ["sha256:installed-image"]}])
                self.assertEqual([(row["file"], row["passed"]) for row in receipt["specs"]],
                                 [(specs[0], False), (specs[1], True)])

    def test_suite_keeps_failed_receipt_when_sources_change_after_passed_specs(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)

            def execute(args, **kwargs):
                output = Path(kwargs["env"]["ORBITJOB_CONSOLE_REPORT_DIR"])
                (output / "report.json").write_text(json.dumps({"suites": [{"specs": [{"tests": [{
                    "expectedStatus": "passed", "results": [{"status": "passed"}]}]}]}]}))
                return SimpleNamespace(returncode=0)

            with mock.patch.object(harness, "source_manifest", side_effect=[{"input": "before"}, {"input": "after"}]), \
                    mock.patch.object(harness, "kube", return_value='{"items": []}'), \
                    mock.patch.object(harness.subprocess, "run", side_effect=execute):
                with self.assertRaisesRegex(RuntimeError, "inputs changed"):
                    harness.run_suite({}, "http://127.0.0.1:8080", directory,
                                      {"prepared": True, "clusterUID": "cluster-uid"}, ["one.spec.ts"])
            self.assertFalse(harness.read_private(directory / "verification.json")["passed"])


if __name__ == "__main__":
    unittest.main()
