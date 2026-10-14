import importlib.util
import json
import base64
import unittest
from pathlib import Path
from unittest.mock import patch
import urllib.parse

spec = importlib.util.spec_from_file_location("prepare_portfolio", Path(__file__).with_name("prepare_portfolio.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PreparePortfolioTests(unittest.TestCase):
    def test_encoded_password_survives_forwarding_without_entering_command_arguments(self):
        original = "postgresql://budget:p%40ss%3A%2F%24@postgres.internal:5432/budget?sslmode=disable"
        result = module.forwarded_dsn(original, 15432)
        url = urllib.parse.urlsplit(result)
        self.assertEqual(url.hostname, "127.0.0.1")
        self.assertEqual(url.port, 15432)
        self.assertEqual(urllib.parse.unquote(url.password), "p@ss:/$")
        self.assertEqual(url.query, "sslmode=disable")

    def test_secret_reference_is_resolved_in_process(self):
        value = "postgres://synthetic:canary@postgres/budget"
        deployment = {"spec": {"template": {"spec": {"containers": [{"name": "budget-app", "env": [{"name": "DATABASE_URL", "valueFrom": {"secretKeyRef": {"name": "database", "key": "dsn"}}}]}]}}}}
        secret = {"data": {"dsn": base64.b64encode(value.encode()).decode()}}
        with patch.object(module, "command", side_effect=[json.dumps(deployment).encode(), json.dumps(secret).encode()]) as command:
            self.assertEqual(module.deployment_dsn(["k3s", "kubectl"], "budget"), value)
            self.assertNotIn("canary", str(command.call_args_list))

    def test_failed_subprocess_does_not_expose_stderr(self):
        from subprocess import CompletedProcess
        with patch.object(module.subprocess, "run", return_value=CompletedProcess([], 1, b"", b"secret-canary")):
            with self.assertRaises(module.Failure) as raised:
                module.command(["tool"], "export")
        self.assertIn("export", str(raised.exception))
        self.assertNotIn("canary", str(raised.exception))

    def test_existing_configmap_reference_is_resolved_without_printing_its_value(self):
        value = "postgres://synthetic:canary@postgres/budget"
        deployment = {"spec": {"template": {"spec": {"containers": [{"name": "budget-app", "env": [{"name": "DATABASE_URL", "valueFrom": {"configMapKeyRef": {"name": "database", "key": "dsn"}}}]}]}}}}
        config = {"data": {"dsn": value}}
        with patch.object(module, "command", side_effect=[json.dumps(deployment).encode(), json.dumps(config).encode()]) as command:
            self.assertEqual(module.deployment_dsn(["k3s", "kubectl"], "budget"), value)
            self.assertNotIn("canary", str(command.call_args_list))


if __name__ == "__main__":
    unittest.main()
