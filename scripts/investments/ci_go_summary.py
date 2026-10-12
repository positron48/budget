#!/usr/bin/env python3
"""Expose bounded synthetic CI test failures as GitHub annotations.

Only use with CI fixture runs; never publish private rehearsal logs.
"""
import collections
import json
from pathlib import Path
import sys


def escaped(value):
    return value.replace('%', '%25').replace('\r', '%0D').replace('\n', '%0A')


def summarize(lines):
    tails = collections.defaultdict(lambda: collections.deque(maxlen=16))
    raw = collections.deque(maxlen=16)
    failures = []
    failed_packages = set()
    for line in lines:
        try:
            event = json.loads(line)
        except ValueError:
            raw.append(line[:2048])
            continue
        if not isinstance(event, dict):
            continue
        package, test = event.get('Package', ''), event.get('Test', '')
        if not isinstance(package, str) or not isinstance(test, str):
            continue
        key = (package, test)
        output = event.get('Output')
        if isinstance(output, str):
            tails[key].append(output[:2048])
        if event.get('Action') in ('fail', 'build-fail'):
            if test:
                failed_packages.add(package)
                failures.append((package + '/' + test, ''.join(tails[key])))
            elif package not in failed_packages:
                failures.append((package, ''.join(raw) + ''.join(tails[key])))
    return ['::error::' + escaped((name + '\n' + output)[:12000]) for name, output in failures[:100]]


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('Expected one synthetic go-test JSON file')
    with Path(sys.argv[1]).open() as stream:
        for annotation in summarize(stream):
            print(annotation)
