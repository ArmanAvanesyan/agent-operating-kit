import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('agy_cycle_test', ROOT / 'scripts/native-loading/probe_agy_discovery_cycle.py')
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)
gate = probe.gate


def evidence():
    valid = b'---\nname: aok-probe\ndescription: Synthetic fixture\n---\nInert\n'
    bad = gate.corrupt(valid)
    report = dict(kind='aok-agy-discovery-cycle/v1', gateContract=gate.CONTRACT, scope='project', version='1.4.2',
                  binarySHA256=gate.PIN, provider='isolated-oauth-account', terminalGeometry={'columns': 160, 'rows': 80},
                  renderedFixtureUTF8=valid.decode(), malformedFixtureUTF8=bad.decode(), fixtureRelativePath=gate.TARGET,
                  aokSHA256='a'*64, guardSHA256='b'*64, sourceProfileRemoved=True, cycles=[])
    for i, order in enumerate(gate.ORDERS):
        cycle = dict(workspaceIdentity=str(i), homeIdentity='home' + str(i), freshSeedCopy=True, privateRootRemoved=True, phases=[])
        report['cycles'].append(cycle)
        for name in order:
            present = name in ('valid', 'repaired')
            sha = None if name == 'absent' else gate.digest(valid if present else bad)
            check = {'status': 'absent'} if name == 'absent' else dict(status='valid' if present else 'invalid',
                exitCode=0 if present else 1, diagnosticCodes=[] if present else ['native.decode'], inputSHA256=sha)
            cycle['phases'].append(dict(phase=name, workspaceIdentity=str(i), homeIdentity='home' + str(i), runnerStarted=True, nativeExecAttempted=True, fixtureRelativePath=gate.TARGET,
                inputSHA256=sha, afterSHA256=sha, guardSHA256='b'*64, guardUnchanged=True, fixtureValidation=check,
                cleanupVerified=True, managerStableSeconds=5.1, managerObservationSeconds=6,
                state=dict(commandSent=True, framedNativeManagerObserved=True, managerSectionObserved=True,
                    guardEntryVisible=True, completeControlledList=True, targetEntryVisible=present,
                    nameObserved=present, descriptionObserved=present, authBarrierObserved=False,
                    errorObserved=False, terminalAmbiguityObserved=False)))
    return report


class DiscoveryGateTests(unittest.TestCase):
    def test_complete_replicated_evidence_is_candidate_without_claiming_parse_error(self):
        report = evidence()
        self.assertEqual(gate.evaluate(report), [])
        self.assertNotIn('parseErrorObserved', report['cycles'][0]['phases'][1]['state'])

    def test_saved_native_evidence_and_archived_sources_are_bound(self):
        directory = ROOT / 'docs/evidence/native-adapters/agy-discovery-cycle-2026-10-18'
        report = json.loads((directory / 'cycles.json').read_text())
        self.assertEqual(gate.evaluate(report), [])
        index = json.loads((directory / 'index.json').read_text())
        for relative, metadata in index['files'].items():
            path = directory / relative
            self.assertTrue(path.resolve().is_relative_to(directory.resolve()))
            data = path.read_bytes()
            self.assertEqual(len(data), metadata['bytes'])
            self.assertEqual(gate.digest(data), metadata['sha256'])
        for cycle in report['cycles']:
            for phase in cycle['phases']:
                for digest in phase['sourceHashes'].values():
                    archived = directory / 'sources' / (digest + '.py')
                    self.assertEqual(gate.digest(archived.read_bytes()), digest)

    def test_controls_independently_fail_closed(self):
        mutations = [
            lambda r: r.update(binarySHA256='c'*64),
            lambda r: r.update(sourceProfileRemoved=False),
            lambda r: r['cycles'].pop(),
            lambda r: r['cycles'][1].update(workspaceIdentity='0'),
            lambda r: r['cycles'][1]['phases'].pop(0),
            lambda r: r['cycles'][0].update(privateRootRemoved=False),
        ]
        for key, value in [('afterSHA256', 'c'*64), ('workspaceIdentity', 'changed'),
                           ('fixtureRelativePath', 'different'), ('guardUnchanged', False),
                           ('cleanupVerified', False), ('homeIdentity', 'replaced'), ('nativeExecAttempted', False), ('runnerStarted', False), ('managerStableSeconds', 4.9),
                           ('managerObservationSeconds', float('nan'))]:
            mutations.append(lambda r, k=key, v=value: r['cycles'][0]['phases'][2].update({k: v}))
        for key, value in [('guardEntryVisible', False), ('completeControlledList', False),
                           ('authBarrierObserved', True), ('errorObserved', True),
                           ('terminalAmbiguityObserved', True), ('targetEntryVisible', False),
                           ('framedNativeManagerObserved', False)]:
            mutations.append(lambda r, k=key, v=value: r['cycles'][0]['phases'][2]['state'].update({k: v}))
        mutations.extend([
            lambda r: r['cycles'][0]['phases'][1]['fixtureValidation'].update(diagnosticCodes=[]),
            lambda r: r['cycles'][0]['phases'][1]['fixtureValidation'].update(inputSHA256='c'*64),
            lambda r: r.update(malformedFixtureUTF8=r['renderedFixtureUTF8']),
        ])
        for mutate in mutations:
            report = evidence()
            mutate(report)
            self.assertTrue(gate.evaluate(report), mutate)

    def test_malformed_evidence_does_not_crash(self):
        for report in (None, [], {}, {'cycles': None}, {'cycles': [None]}):
            self.assertTrue(gate.evaluate(report))

    def test_mutation_is_one_byte_inside_frontmatter(self):
        valid = evidence()['renderedFixtureUTF8'].encode()
        bad = gate.corrupt(valid)
        self.assertEqual(len(bad), len(valid)+1)
        self.assertEqual(bad.replace(b'description: [', b'description: ', 1), valid)
        with self.assertRaises(ValueError):
            gate.corrupt(b'---\nname: aok-probe\n---\ndescription: body\n')

    def test_complete_controlled_list_rejects_unknown_duplicate_wrapped_rows(self):
        prefix = 'Available Agents\n> ● default  Default agent\n  ○ guard  Guard fixture\n'
        footer = '\nKeyboard: ↑/↓ Navigate  enter Select / Toggle  k Kill Active Subagent  esc Go Back'
        def parse(extra):
            return probe.controlled_list(prefix + extra + footer, 'guard', 'Guard fixture', 'Target fixture')
        self.assertTrue(parse('  ○ aok-probe  Target fixture')['targetEntryVisible'])
        self.assertTrue(parse('')['completeControlledList'])
        for extra in ('○ unexpected  Unknown', '○ guard  Guard fixture', '○ aok-probe\nTarget fixture',
                      '○ aok-probe  Wrong description', '...'):
            self.assertFalse(parse(extra)['completeControlledList'])

    def test_preflight_profile_uses_supported_terminal_without_credentials(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td).resolve()
            cli = root / 'antigravity'
            cli.touch()
            profile = probe.login.sandbox_profile(root, cli, '/dev/tty')
            self.assertIn('(literal "/dev/tty")', profile)
            with self.assertRaises(ValueError):
                probe.login.sandbox_profile(root, cli, '/dev/null')

    def test_aok_inspect_rejects_one_byte_corruption(self):
        binary = Path('/private/tmp/aok-agy-malformed-aok')
        if not binary.exists():
            self.skipTest('optional locally built AOK unavailable')
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'aok-home').mkdir()
            valid = evidence()['renderedFixtureUTF8'].encode()
            self.assertEqual(probe.validation(binary, root, valid)['status'], 'valid')
            bad = probe.validation(binary, root, gate.corrupt(valid))
            self.assertEqual((bad['status'], bad['exitCode']), ('invalid', 1))
            self.assertIn('native.decode', bad['diagnosticCodes'])
