import hashlib
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

# The operator modules are shipped together and imported from their own folder.
sys.path.insert(0, str(Path(__file__).parent))
spec = importlib.util.spec_from_file_location("rebuild_portfolio", Path(__file__).with_name("rebuild_portfolio.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class RebuildPortfolioTests(unittest.TestCase):
    def test_unreviewed_or_public_input_is_rejected_before_any_database_command(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "source.json"
            path.write_bytes(b"private original")
            path.chmod(0o600)
            sha = hashlib.sha256(path.read_bytes()).hexdigest()
            self.assertEqual(module.checked_file(path, sha), path.resolve())
            with self.assertRaises(module.Failure):
                module.checked_file(path, "0" * 64)
            path.chmod(0o644)
            with self.assertRaises(module.Failure):
                module.checked_file(path, sha)

    def test_incomplete_or_different_scope_candidate_cannot_authorize_replacement(self):
        scope = {"StateSHA256": "before", "Scope": {"Accounts": [{"ID": "same-account"}]}}
        report = {"Status": "isolated_portfolio_candidate_prepared", "SourceWrites": False,
                  "SchemaVersion": 61, "BeforeStateSHA256": "before", "Accounts": 1,
                  "Operations": 2, "FreshQuotes": 1, "FXChecks": 9}
        module.require_report(report, scope)
        for field, wrong in (("SourceWrites", True), ("BeforeStateSHA256", "other"), ("Accounts", 2),
                             ("SchemaVersion", 60), ("Operations", 0), ("FreshQuotes", 0), ("FXChecks", 0)):
            with self.subTest(field=field), self.assertRaises(module.Failure):
                module.require_report({**report, field: wrong}, scope)


if __name__ == "__main__":
    unittest.main()
