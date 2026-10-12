#!/usr/bin/env python3
"""Pin public tracked/untracked source for a dirty release candidate; no publishing.

Private fixtures, credentials, caches and symlinks are rejected when Git would
include them. The manifest is an inventory, not a test/deployment certificate.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess

ROOT = Path(__file__).resolve().parents[2]
BLOCKED = {'.git', '.data', '.codex', '.agents', '.claude', 'node_modules', '.next',
           'test-results', 'playwright-report', '__pycache__'}


def encode(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def collect(root):
    names = subprocess.run(['git', 'ls-files', '-z', '--cached', '--others', '--exclude-standard'],
                           cwd=root, check=True, capture_output=True).stdout.split(b'\0')
    files = []
    for name in sorted(set(n.decode('utf-8') for n in names if n)):
        path = Path(name)
        if path.is_absolute() or '..' in path.parts or path.parts[:3] == ('docs', 'tasks', 'investments') or path.as_posix() == 'deploy/investments/README.md' or any(
                part in BLOCKED or part == '.env' or part.startswith('.env.') for part in path.parts):
            raise ValueError('private or generated path is included by Git')
        full = root / path
        if not full.exists() and not full.is_symlink():
            continue  # A tracked deletion is represented by absence from inventory.
        if any((root / Path(*path.parts[:i])).is_symlink() for i in range(1, len(path.parts) + 1)):
            raise ValueError('source symlink is not permitted')
        info = full.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise ValueError('source must be a regular file')
        raw = full.read_bytes()
        files.append({'Path': name, 'SHA256': sha(raw), 'Size': len(raw),
                      'Executable': bool(info.st_mode & 0o111)})
    if not files:
        raise ValueError('empty source inventory')
    base = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=root, check=True,
                          capture_output=True, text=True).stdout.strip()
    return {'Version': 1, 'BaseCommit': base, 'Files': files, 'SourceSHA256': sha(encode(files))}


def verify(root, manifest):
    if (set(manifest) != {'Version', 'BaseCommit', 'Files', 'SourceSHA256'}
            or manifest['Version'] != 1 or manifest['SourceSHA256'] != sha(encode(manifest['Files']))):
        raise ValueError('invalid source manifest')
    current = collect(root)
    if current['Files'] != manifest['Files']:
        raise ValueError('source changed since candidate inventory')
    return current


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    choice = parser.add_mutually_exclusive_group(required=True)
    choice.add_argument('--out', type=Path)
    choice.add_argument('--verify', type=Path)
    args = parser.parse_args()
    try:
        if args.verify:
            info = args.verify.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_size > 8 * 1024 * 1024:
                raise ValueError('manifest must be a private regular file')
            result = verify(ROOT, json.loads(args.verify.read_bytes()))
        else:
            if not args.out.parent.is_dir() or args.out.parent.stat().st_mode & 0o077:
                raise ValueError('output parent must already be private (0700)')
            result = collect(ROOT)
            fd = os.open(args.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'wb') as stream:
                stream.write(encode(result) + b'\n')
                stream.flush()
                os.fsync(stream.fileno())
        print(json.dumps({'Files': len(result['Files']), 'SourceSHA256': result['SourceSHA256']}))
    except (OSError, ValueError, subprocess.SubprocessError):
        raise SystemExit('Source inventory failed; no publishing or deployment performed.') from None


if __name__ == '__main__':
    main()
