import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from experiments.parser.study_v2_runner import (
    proposals, source_hash, source_lines, validate_dispatch, file_sha, catalog_run, date_value)
from experiments.parser.study_v2_evaluation import freeze_protocol, digest
from experiments.parser.tests.test_study_v2_evaluation import fixture, EXECUTION


def case(text, split='train'):
    result = dict(id='synthetic', group='group', split=split,
                  notes=[{'name': 'notes.txt', 'text': text}], files=['d1t01.flac'])
    result['source_input_sha256'] = source_hash(result)
    return result


def predictions(c, roles):
    return [{'note': 'notes.txt', 'line_index': i, 'roles': r, 'scores': {}} for i, r in enumerate(roles)]


def fixed():
    return dict(metadata=dict(artist='', date='', venue='', city=''), ready=False,
                review_reasons=['missing metadata'], mappings={}, write_plan={'writes': []})


class ProposalTests(unittest.TestCase):
    def test_unicode_source_span_and_no_underlying_output_mutation(self):
        c = case('  Artist: Bänd\n2020-01-01\nHåll\nTown\n\n01. Song')
        pred = predictions(c, [['artist'], ['date'], ['venue'], ['city'], [], ['title']])
        out = fixed()
        before = copy.deepcopy(out)
        got = proposals(c, pred, out, lambda x: x)
        self.assertEqual({x['field']: x['value'] for x in got},
                         dict(artist='Bänd', date='2020-01-01', venue='Håll', city='Town'))
        artist = got[0]
        self.assertEqual(c['notes'][0]['text'].splitlines()[0][artist['start']:artist['end']], 'Bänd')
        self.assertEqual(out, before)

    def test_historical_guest_uncertainty_technical_multirole_and_body_abstain(self):
        for text, role in [('LTP 2019-01-01', ['date']), ('Guest: Other Band', ['artist']),
                           ('Probably The Hall', ['venue']), ('Transfer: 2020-01-01', ['date']),
                           ('Town @ Hall', ['city', 'venue']), ('Personnel: Other Band', ['artist']),
                           ('With Another Band', ['artist'])]:
            c = case(text)
            self.assertEqual(proposals(c, predictions(c, [role]), fixed(), lambda x: x), [], text)
        c = case('Band\n\nGuest Person\nOld City')
        self.assertEqual([p['field'] for p in proposals(c, predictions(c, [['artist'], [], ['artist'], ['city']]), fixed(), lambda x: x)], ['artist'])

    def test_ready_cases_never_get_assistance_and_conflicting_sources_abstain(self):
        c = case('Band')
        out = fixed();out['ready'] = True
        self.assertEqual(proposals(c, predictions(c, [['artist']]), out, lambda x:x), [])
        c['notes'].append({'name': 'other.txt', 'text': 'Other Band'})
        pred = predictions(c, [['artist']])+[{'note': 'other.txt', 'line_index': 0, 'roles': ['artist']}]
        self.assertEqual(proposals(c, pred, fixed(), lambda x:x), [])
        populated = fixed();populated['metadata']['artist'] = 'Band'
        self.assertEqual(proposals(c, pred, populated, lambda x:x), [])

    def test_numeric_dates_reach_deterministic_validator(self):
        c = case('10-29-2011')
        self.assertEqual(proposals(c, predictions(c, [['date']]), fixed(), lambda x: '2011-10-29')[0]['value'], '2011-10-29')
        self.assertEqual(proposals(c, predictions(c, [['date']]), fixed(), lambda x: ''), [])

    def test_source_line_normalization_is_explicit(self):
        self.assertEqual(source_lines('\ufeffBand\r\n\rCity\r\n'), ['Band', '', 'City'])
        self.assertEqual(source_lines(''), [''])
        self.assertEqual(source_lines('\n'), [''])

    def test_gold_flags_and_reference_metadata_cannot_influence_proposals(self):
        c = case('Band')
        pred = predictions(c, [['artist']])
        expected = proposals(c, pred, fixed(), lambda x:x)
        c['oracle'] = {'artist': 'Invented'}
        c['provenance'] = {'artist': 'Invented'}
        c['mixed_scope'] = True
        self.assertEqual(proposals(c, pred, fixed(), lambda x:x), expected)


class DispatchTests(unittest.TestCase):
    def test_test_dispatch_requires_reviewed_freeze_before_any_inference(self):
        with tempfile.TemporaryDirectory() as tmp:
            model = Path(tmp)/'model.json';model.write_text('{}')
            with self.assertRaisesRegex(ValueError, 'freeze required'):
                validate_dispatch([case('Band', 'test')], 'test', model, EXECUTION)
            selected, key = validate_dispatch([case('Band', 'test'), case('Band')], 'train', model, EXECUTION)
            self.assertEqual(len(selected), 1)
            self.assertEqual(selected[0]['split'], 'train')
            self.assertEqual(len(key), 64)

    def test_changed_sources_rejected_before_dispatch(self):
        with tempfile.TemporaryDirectory() as tmp:
            model = Path(tmp)/'model.json';model.write_text('{}')
            c = case('Band');c['notes'][0]['text'] = 'Changed'
            with self.assertRaisesRegex(ValueError, 'source hash'):
                validate_dispatch([c], 'train', model, EXECUTION)

    def test_valid_test_freeze_and_tampered_execution(self):
        with tempfile.TemporaryDirectory() as tmp:
            model = Path(tmp)/'model.json';model.write_text('{}')
            c = case('Band', 'test')
            sources = [dict(id=c['id'], group=c['group'], source_input_sha256=source_hash(c), notes={'notes.txt': 1}, files=c['files'])]
            gold = [dict(id=c['id'], source_input_sha256=source_hash(c), label_status='agent-reviewed',
                         metadata={f: '' for f in ('artist','date','venue','city')}, review_required=True)]
            lines = [dict(id=c['id'], note='notes.txt', line_index=0, line='Band', roles=['artist'], role_disputed=False,
                          source_input_sha256=source_hash(c), label_status='agent-reviewed',
                          mixed_scope=False, uncertainty=False, conflict=False)]
            protocol = freeze_protocol(sources, gold, lines, classifier_model_sha256=file_sha(model), execution_manifest=EXECUTION)
            selected, key = validate_dispatch([c], 'test', model, EXECUTION, protocol, gold, lines)
            self.assertEqual(key, digest(protocol))
            changed = dict(EXECUTION, runner_sha256='d'*64)
            with self.assertRaisesRegex(ValueError, 'identity mismatch'):
                validate_dispatch([c], 'test', model, changed, protocol, gold, lines)
            # A claimed source hash does not authenticate mutated label text.
            altered = copy.deepcopy(lines);altered[0]['line'] = 'Different Band'
            altered_protocol = freeze_protocol(sources, gold, altered, classifier_model_sha256=file_sha(model), execution_manifest=EXECUTION)
            with self.assertRaisesRegex(ValueError, 'source bytes'):
                validate_dispatch([c], 'test', model, EXECUTION, altered_protocol, gold, altered)
            wrong_inventory = copy.deepcopy(sources);wrong_inventory[0]['files'] = ['invented.flac']
            wrong_protocol = freeze_protocol(wrong_inventory, gold, lines, classifier_model_sha256=file_sha(model), execution_manifest=EXECUTION)
            with self.assertRaisesRegex(ValueError, 'inventory mismatch'):
                validate_dispatch([c], 'test', model, EXECUTION, wrong_protocol, gold, lines)

    def test_catalog_receives_no_gold_or_reference_metadata(self):
        c = case('Band');c.update(oracle={'artist':'secret answer'}, provenance={'artist':'secret reference'})
        raw = {'id': c['id'], 'artist':'', 'date':'', 'venue':'', 'city':'', 'review':True,
               'status':'needs-review', 'review_reasons':['missing'], 'titles':{}, 'mappings':{}}
        with patch('experiments.parser.study_v2_runner.subprocess.run') as run:
            run.return_value.stdout=json.dumps(raw)
            result = catalog_run('/fake', c)
            payload = json.loads(run.call_args.kwargs['input'])
            self.assertEqual(set(payload), {'id', 'notes', 'files'})
            self.assertFalse(result['ready'])
            self.assertEqual(result['write_plan']['writes'], [])

    def test_malformed_date_helper_fails_closed(self):
        with patch('experiments.parser.study_v2_runner.subprocess.run') as run:
            run.return_value.stdout=json.dumps({'date': 12, 'review_reasons': None})
            with self.assertRaisesRegex(ValueError, 'malformed date'):
                date_value('/fake', '2020-01-01')


if __name__ == '__main__':
    unittest.main()
