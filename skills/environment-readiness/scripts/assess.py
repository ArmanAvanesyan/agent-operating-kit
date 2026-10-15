#!/usr/bin/env python3
"""Assess supplied readiness evidence without running setup or project commands."""
from __future__ import annotations

import argparse
import json
from pathlib import Path

PROFILES = {'inspection', 'local-development', 'service-integration',
            'application-verification', 'delivery'}
OUTCOMES = {'pass', 'fail', 'blocked', 'not-checked', 'not-applicable'}
DIMENSIONS = {'context', 'verification', 'feedback'}


def assess(report: dict, expected_revision: str | None = None) -> dict:
    if (not isinstance(report, dict) or not isinstance(report.get('profile'), str)
            or report['profile'] not in PROFILES):
        raise ValueError('unknown or missing profile')
    for field in ('task', 'revision', 'environment'):
        if not isinstance(report.get(field), str) or not report[field].strip():
            raise ValueError(f'{field} must be a nonempty string')
    checks = report.get('checks')
    if not isinstance(checks, list) or not checks:
        raise ValueError('checks must be a nonempty array')
    required_dimensions = {'context'} if report['profile'] == 'inspection' else DIMENSIONS
    seen, covered, issues = set(), set(), []
    failed = False
    if expected_revision and report['revision'] != expected_revision:
        issues.append({'id': 'revision', 'reason': 'stale-evidence'})
    for check in checks:
        if not isinstance(check, dict):
            raise ValueError('each check must be an object')
        identity = check.get('id')
        if not isinstance(identity, str) or not identity.strip() or identity in seen:
            raise ValueError('check IDs must be nonempty and unique')
        seen.add(identity)
        if (not isinstance(check.get('dimension'), str) or check['dimension'] not in DIMENSIONS
                or not isinstance(check.get('outcome'), str) or check['outcome'] not in OUTCOMES):
            raise ValueError(f'{identity}: invalid dimension or outcome')
        if not isinstance(check.get('required'), bool):
            raise ValueError(f'{identity}: required must be explicit boolean')
        evidence = check.get('evidence', [])
        if not isinstance(evidence, list) or any(not isinstance(x, str) or not x.strip() for x in evidence):
            raise ValueError(f'{identity}: evidence must contain nonempty references')
        if check['outcome'] == 'not-applicable' and (
                not isinstance(check.get('rationale'), str) or not check['rationale'].strip()):
            raise ValueError(f'{identity}: not-applicable requires rationale')
        if not check['required']:
            continue
        covered.add(check['dimension'])
        if check['outcome'] == 'fail':
            failed = True
        if check['outcome'] != 'pass':
            issues.append({'id': identity, 'reason': check['outcome']})
        elif not evidence:
            issues.append({'id': identity, 'reason': 'missing-evidence'})
    for dimension in sorted(required_dimensions - covered):
        issues.append({'id': dimension, 'reason': 'missing-required-dimension'})
    return {'kind': 'aok-readiness-assessment', 'version': 1,
            'profile': report['profile'], 'task': report['task'],
            'revision': report['revision'], 'environment': report['environment'],
            'verdict': 'not-ready' if failed else 'blocked' if issues else 'ready',
            'issues': issues, 'checks': checks,
            'limitations': ['Checks consistency of supplied records, not truth or freshness of referenced artifacts.',
                            'Caller must enumerate every task-required capability; no setup commands were run.']}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('report', type=Path)
    parser.add_argument('--expected-revision')
    args = parser.parse_args()
    try:
        result = assess(json.loads(args.report.read_text()), args.expected_revision)
    except (OSError, ValueError) as error:
        parser.error(str(error))
    print(json.dumps(result, indent=2))
    raise SystemExit(0 if result['verdict'] == 'ready' else 1)


if __name__ == '__main__':
    main()
