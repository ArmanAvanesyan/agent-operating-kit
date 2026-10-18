"""Versioned observable project-discovery gate; never authorizes execution.

This is separate from the historical explicit-parser-error comparator. A passing
result is a candidate for independent review, not automatic descriptor promotion.
"""
import hashlib
import re

CONTRACT = 'agy-project-discovery/v2'
TARGET = '.agents/agents/aok-probe/agent.md'
PIN = 'fa85a90abded17bcd1b185bbd1d6b1b82065f093ad4700d4dbacd3be2433294c'
ORDERS = (('valid', 'malformed', 'repaired', 'absent'),
          ('cold-malformed', 'valid', 'malformed', 'repaired', 'absent'))


def digest(data):
    return hashlib.sha256(data).hexdigest()


def corrupt(valid):
    """One inserted byte in frontmatter; metadata/body otherwise identical."""
    marker = b'\ndescription: '
    if not valid.startswith(b'---\n') or valid.count(marker) != 1:
        raise ValueError('unexpected controlled fixture')
    end = valid.find(b'\n---', 4)
    at = valid.index(marker) + len(marker)
    if end < at:
        raise ValueError('description not inside frontmatter')
    return valid[:at] + b'[' + valid[at:]


def _evaluate(report):
    """Fail closed on missing controls; return stable diagnostic codes only."""
    problems = []
    if (report.get('kind') != 'aok-agy-discovery-cycle/v1' or
            report.get('gateContract') != CONTRACT or report.get('scope') != 'project' or
            report.get('version') != '1.4.2' or report.get('binarySHA256') != PIN or
            report.get('provider') != 'isolated-oauth-account' or
            report.get('terminalGeometry') != {'columns': 160, 'rows': 80}):
        problems.append('gate.binding')
    try:
        valid = report['renderedFixtureUTF8'].encode('utf-8')
        malformed = report['malformedFixtureUTF8'].encode('utf-8')
        if corrupt(valid) != malformed:
            problems.append('gate.mutation')
        expected = {'valid': digest(valid), 'repaired': digest(valid),
                    'malformed': digest(malformed), 'cold-malformed': digest(malformed), 'absent': None}
    except (KeyError, AttributeError, ValueError):
        return ['gate.fixture']
    if (report.get('fixtureRelativePath') != TARGET or
            not re.fullmatch('[0-9a-f]{64}', report.get('aokSHA256', '')) or
            not re.fullmatch('[0-9a-f]{64}', report.get('guardSHA256', '')) or
            report.get('sourceProfileRemoved') is not True):
        problems.append('gate.provenance')
    cycles = report.get('cycles', [])
    if len(cycles) != 2:
        return problems + ['gate.replication']
    identities = []
    homes = []
    for cycle, order in zip(cycles, ORDERS):
        identity = cycle.get('workspaceIdentity')
        home = cycle.get('homeIdentity')
        if not home or home in homes:
            problems.append('gate.home')
        homes.append(home)
        if not identity or identity in identities or cycle.get('freshSeedCopy') is not True:
            problems.append('gate.workspace')
        identities.append(identity)
        phases = cycle.get('phases', [])
        if (tuple(p.get('phase') for p in phases) != order or
                cycle.get('privateRootRemoved') is not True):
            problems.append('gate.order-or-cleanup')
            continue
        for phase in phases:
            name = phase['phase']
            state, validation = phase.get('state', {}), phase.get('fixtureValidation', {})
            present = name in ('valid', 'repaired')
            if (phase.get('workspaceIdentity') != identity or phase.get('homeIdentity') != home or phase.get('fixtureRelativePath') != TARGET or
                    phase.get('inputSHA256') != expected[name] or phase.get('afterSHA256') != expected[name] or
                    phase.get('guardSHA256') != report.get('guardSHA256') or
                    phase.get('guardUnchanged') is not True):
                problems.append('gate.input-integrity')
            if name != 'absent' and validation.get('inputSHA256') != expected[name]:
                problems.append('gate.fixture-validation')
            if name == 'absent':
                if validation.get('status') != 'absent':
                    problems.append('gate.fixture-validation')
            elif present:
                if validation.get('status') != 'valid' or validation.get('exitCode') != 0:
                    problems.append('gate.fixture-validation')
            elif (validation.get('status') != 'invalid' or validation.get('exitCode') != 1 or
                  'native.decode' not in validation.get('diagnosticCodes', [])):
                problems.append('gate.fixture-validation')
            if (phase.get('cleanupVerified') is not True or phase.get('runnerStarted') is not True or
                    phase.get('nativeExecAttempted') is not True or
                    phase.get('managerStableSeconds', 0) < 5 or phase.get('managerObservationSeconds', 0) < 5 or
                    state.get('commandSent') is not True or state.get('framedNativeManagerObserved') is not True or
                    state.get('managerSectionObserved') is not True or state.get('guardEntryVisible') is not True or
                    state.get('completeControlledList') is not True or
                    state.get('targetEntryVisible') is not present or
                    state.get('nameObserved') is not present or state.get('descriptionObserved') is not present or
                    any(state.get(key) is not False for key in
                        ('authBarrierObserved', 'errorObserved', 'terminalAmbiguityObserved'))):
                problems.append('gate.native-observation')
    return sorted(set(problems))


def evaluate(report):
    """Malformed evidence never raises or qualifies, including nonfinite timings."""
    import math
    try:
        for cycle in report.get('cycles', []):
            for phase in cycle.get('phases', []):
                for key in ('managerStableSeconds', 'managerObservationSeconds'):
                    value = phase.get(key)
                    if type(value) not in (int, float) or not math.isfinite(value):
                        return ['gate.timing']
        return _evaluate(report)
    except (TypeError, AttributeError, KeyError, ValueError):
        return ['gate.evidence-shape']
