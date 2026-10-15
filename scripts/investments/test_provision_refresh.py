import base64
import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("provision_refresh", Path(__file__).with_name("provision_refresh.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
TENANT = "00000000-0000-4000-8000-000000000001"


class ProvisionRefreshTests(unittest.TestCase):
    def worker_responses(self, unusable=0, alpha_success=1):
        image="ghcr.io/example/budget-app@sha256:synthetic"
        app={"spec":{"template":{"spec":{"containers":[{"name":"budget-app","image":image}]}}}}
        responses=[json.dumps(app).encode()]
        for worker in ("marketdata","alphafx","investcbr"):
            pod={"metadata":{"name":"synthetic-"+worker},"spec":{"containers":[{"image":image}]},"status":{"containerStatuses":[{"ready":True}],"conditions":[{"type":"Ready","status":"True"}]}}
            responses.append(json.dumps({"items":[pod]}).encode())
            body='budget_investment_worker_snapshot_success{worker="'+worker+'"} 1\n'
            if worker=="marketdata":
                body+='budget_investment_worker_source_enabled{worker="marketdata"} 1\n'
                body+='budget_investment_worker_job_attempts_total{kind="last_price",result="ready",worker="marketdata"} 10\n'
                for state,value in (("fresh",7),("expired",0),("missing",unusable)):
                    body+='budget_investment_worker_mapped_quote_endpoints{state="'+state+'",worker="marketdata"} '+str(value)+'\n'
            else:
                body+='budget_investment_worker_iterations_total{result="success",worker="'+worker+'"} '+str(alpha_success if worker=="alphafx" else 1)+'\n'
            if worker=="alphafx":
                for state,value in (("fresh",2),("expired",0),("missing",0)):
                    body+='budget_investment_worker_fx_endpoints{state="'+state+'",worker="alphafx"} '+str(value)+'\n'
            responses.append(body.encode())
        return responses

    def test_ready_pods_and_successful_jobs_do_not_hide_unusable_prices(self):
        for unusable,alpha_success,expected in ((0,1,True),(1,1,False),(0,0,False)):
            with self.subTest(unusable=unusable,alpha_success=alpha_success), patch.object(module,"command",side_effect=self.worker_responses(unusable,alpha_success)):
                result=module.worker_status(["kubectl"],"budget")
            self.assertTrue(result["WorkersReadyVerified"])
            self.assertEqual(result["WorkersOperationalVerified"],expected)
            self.assertFalse(result["FullQuoteCoverageVerified"])

    def test_metrics_reject_nonfinite_values_and_wrong_worker(self):
        self.assertIsNone(module.metric('metric{worker="other"} 1',"metric",{"worker":"marketdata"}))
        self.assertIsNone(module.metric('metric{worker="marketdata"} NaN',"metric",{"worker":"marketdata"}))
        self.assertEqual(module.metric('metric{worker="marketdata"} 7',"metric",{"worker":"marketdata"}),7)

    def test_missing_fx_diagnostics_do_not_prove_zero_missing_pairs(self):
        responses=self.worker_responses()
        responses[4]=responses[4].replace(b'budget_investment_worker_fx_endpoints{state="missing",worker="alphafx"} 0\n',b'')
        with patch.object(module,"command",side_effect=responses):
            result=module.worker_status(["kubectl"],"budget")
        self.assertFalse(result["WorkersOperationalVerified"])
        self.assertIsNone(result["Workers"][1]["MissingOrExpiredFXPairs"])

    def test_keys_only_enter_kubernetes_stdin_and_never_the_report_or_argv(self):
        keys = ["synthetic-tinvest-canary", "synthetic-alpha-canary"]
        with patch.object(module, "command", side_effect=[b"", b"secret created"]) as command, \
                patch.object(module, "hidden_key", side_effect=[base64.b64encode(x.encode()).decode() for x in keys]):
            report = module.provision(["k3s", "kubectl"], "budget", TENANT)
        args, stage, raw = command.call_args.args
        self.assertEqual(args[-3:], ["create", "-f", "-"])
        secret = json.loads(raw)
        self.assertEqual(base64.b64decode(secret["data"]["TENANT_ID"]).decode(), TENANT)
        for key in keys:
            self.assertNotIn(key, str(args))
            self.assertNotIn(key, json.dumps(report))
        self.assertNotIn("last-applied", json.dumps(secret))
        self.assertFalse(report["FinancialWrites"])
        self.assertFalse(report["DeploymentWrites"])

    def test_owned_existing_keys_are_reused_without_input_or_mutation(self):
        secret = {"type": "Opaque", "metadata": {"labels": {"app.kubernetes.io/part-of": module.OWNER}},
                  "data": {"TENANT_ID": base64.b64encode(TENANT.encode()).decode(),
                           "TINVEST_TOKEN": "existing-encoded-token", "ALPHAVANTAGE_API_KEY": "existing-encoded-key"}}
        with patch.object(module, "command", return_value=json.dumps(secret).encode()) as command, \
                patch.object(module, "hidden_key") as hidden:
            report = module.provision(["kubectl"], "budget", TENANT)
        self.assertEqual(command.call_count, 1)
        hidden.assert_not_called()
        self.assertTrue(report["ExistingCredentialsReused"])

    def test_native_mode_adds_only_missing_key_without_exposing_it(self):
        secret = {"type": "Opaque", "metadata": {"resourceVersion": "7", "labels": {"app.kubernetes.io/part-of": module.OWNER}},
                  "data": {"TENANT_ID": base64.b64encode(TENANT.encode()).decode(),
                           "TINVEST_TOKEN": "existing-token", "ALPHAVANTAGE_API_KEY": "existing-alpha"}}
        canary = "native-key-canary"
        with patch.object(module, "command", side_effect=[json.dumps(secret).encode(), b"updated"]) as command, \
                patch.object(module, "hidden_key", return_value=base64.b64encode(canary.encode()).decode()) as hidden:
            report = module.provision(["kubectl"], "budget", TENANT, native=True)
        hidden.assert_called_once()
        argv, _, raw = command.call_args.args
        payload = json.loads(raw)
        self.assertEqual(payload["data"]["TINVEST_TOKEN"], "existing-token")
        self.assertEqual(payload["metadata"]["resourceVersion"], "7")
        self.assertEqual(base64.b64decode(payload["data"]["TWELVEDATA_API_KEY"]).decode(), canary)
        self.assertNotIn(canary, str(argv) + json.dumps(report))
        self.assertEqual(report["Providers"], ["tinvest", "alphavantage", "moex", "twelvedata"])
        self.assertTrue(report["SecretUpdated"])
        self.assertFalse(report["DeploymentWrites"])

    def test_empty_native_responses_do_not_prove_operational_updates(self):
        for successful in (0, 1):
            responses = self.worker_responses()
            for worker in ("moexhistory", "equityhistory"):
                image = "ghcr.io/example/budget-app@sha256:synthetic"
                pod = {"metadata": {"name": worker}, "spec": {"containers": [{"image": image}]},
                       "status": {"containerStatuses": [{"ready": True}], "conditions": [{"type": "Ready", "status": "True"}]}}
                responses.append(json.dumps({"items": [pod]}).encode())
                metrics = 'budget_investment_worker_snapshot_success{worker="'+worker+'"} 1\n'
                metrics += 'budget_investment_worker_source_enabled{worker="'+worker+'"} 1\n'
                metrics += 'budget_investment_worker_job_attempts_total{kind="daily_candle",result="empty",worker="'+worker+'"} 20\n'
                metrics += 'budget_investment_worker_job_attempts_total{kind="daily_candle",result="ready",worker="'+worker+'"} '+str(successful)+'\n'
                responses.append(metrics.encode())
            with patch.object(module, "command", side_effect=responses):
                result = module.worker_status(["kubectl"], "budget", native=True)
            self.assertEqual(result["WorkersOperationalVerified"], bool(successful))
            self.assertEqual(len(result["Workers"]), 5)

    def test_other_tenant_secret_is_never_overwritten(self):
        secret = {"type": "Opaque", "metadata": {"labels": {"app.kubernetes.io/part-of": module.OWNER}},
                  "data": {"TENANT_ID": base64.b64encode(b"different-tenant").decode()}}
        with patch.object(module, "command", return_value=json.dumps(secret).encode()) as command, \
                patch.object(module, "hidden_key") as hidden, self.assertRaises(module.Failure):
            module.provision(["kubectl"], "budget", TENANT)
        self.assertEqual(command.call_count, 1)
        hidden.assert_not_called()

    def test_noninteractive_input_never_falls_back_to_visible_prompt(self):
        with patch.object(module.sys.stdin, "isatty", return_value=False), \
                patch.object(module.getpass, "getpass") as prompt, self.assertRaises(module.Failure):
            module.hidden_key("key", 256)
        prompt.assert_not_called()

    def test_api_failure_does_not_print_credentials_from_stderr(self):
        with patch.object(module.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, b"", b"secret-canary")):
            with self.assertRaises(module.Failure) as raised:
                module.command(["kubectl", "create", "-f", "-"], "create Secret", b"synthetic-key")
        self.assertNotIn("canary", str(raised.exception))


if __name__ == "__main__":
    unittest.main()
