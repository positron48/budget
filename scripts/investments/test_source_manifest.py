import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from source_manifest import collect, verify


class SourceManifestTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.git('init', '-q')
        self.git('config', 'user.email', 'fixture@example.test')
        self.git('config', 'user.name', 'Synthetic fixture')
        (self.root / '.gitignore').write_text('.data/\n.env\n')
        (self.root / 'main.go').write_text('package main\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'Synthetic source')

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.root, check=True,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def test_roundtrip_keeps_untracked_source_and_ignores_private_fixture(self):
        (self.root / 'new.go').write_text('package main\n')
        manifest = json.loads(json.dumps(collect(self.root)))
        (self.root / '.data').mkdir()
        (self.root / '.data/private.json').write_text('private fixture must never be read')
        self.assertEqual(manifest['SourceSHA256'], verify(self.root, manifest)['SourceSHA256'])
        self.assertIn('new.go', [f['Path'] for f in manifest['Files']])

    def test_changes_additions_deletions_and_executable_bit_invalidate(self):
        for change in ('edit', 'add', 'delete', 'mode'):
            with self.subTest(change=change):
                file = self.root / 'main.go'
                file.write_text('package main\n')
                file.chmod(0o644)
                (self.root / 'added.go').unlink(missing_ok=True)
                manifest = collect(self.root)
                if change == 'edit': file.write_text('package changed\n')
                elif change == 'add': (self.root / 'added.go').write_text('package main\n')
                elif change == 'delete': file.unlink()
                else: file.chmod(0o755)
                with self.assertRaises(ValueError): verify(self.root, manifest)

    def test_forced_private_path_is_rejected(self):
        (self.root / '.env').write_text('synthetic secret')
        self.git('add', '-f', '.env')
        with self.assertRaises(ValueError): collect(self.root)

    def test_local_assistant_plan_is_rejected_even_without_ignore(self):
        folder = self.root / '.claude/plans'
        folder.mkdir(parents=True)
        (folder / 'unrelated.md').write_text('unrelated local plan')
        with self.assertRaises(ValueError): collect(self.root)

    def test_personal_investment_research_is_rejected(self):
        folder = self.root / 'docs/tasks/investments'
        folder.mkdir(parents=True)
        (folder / 'README.md').write_text('synthetic personal research')
        with self.assertRaises(ValueError): collect(self.root)

    def test_local_operator_report_is_rejected(self):
        folder = self.root / 'deploy/investments'
        folder.mkdir(parents=True)
        (folder / 'README.md').write_text('synthetic local acceptance report')
        with self.assertRaises(ValueError): collect(self.root)

    def test_symlink_cannot_read_outside_source(self):
        outside = Path(self.tmp.name).parent / (self.root.name + '-outside')
        outside.write_text('synthetic outside data')
        self.addCleanup(outside.unlink)
        (self.root / 'source-link').symlink_to(outside)
        with self.assertRaises(ValueError): collect(self.root)

    def test_modified_manifest_hash_is_rejected(self):
        manifest = collect(self.root)
        manifest['Files'][0]['SHA256'] = '0' * 64
        with self.assertRaises(ValueError): verify(self.root, manifest)


if __name__ == '__main__':
    unittest.main()
