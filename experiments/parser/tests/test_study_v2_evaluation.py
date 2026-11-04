import copy
import unittest
from experiments.parser.study_v2_evaluation import (
    FIELDS, BASELINE_COMMIT, FIXED_COMMIT, digest, freeze_protocol, evaluate)

MODEL = 'b' * 64
EXECUTION = {k: 'c'*64 for k in ('runner_sha256', 'build_manifest_sha256', 'baseline_binary_sha256', 'fixed_binary_sha256', 'classifier_binary_sha256')}


def fixture(count=200):
    sources = []
    gold = []
    lines = []
    for i in range(count):
        case = f'case-{i}'
        metadata = dict(artist='Band', date='2020-01-01', venue='Håll', city='Town')
        sources.append(dict(id=case, group=case, source_input_sha256='a'*64,
                            notes={'notes.txt': 5}, split='test'))
        gold.append(dict(id=case, source_input_sha256='a'*64, label_status='agent-reviewed',
                         metadata=metadata, review_required=True))
        for j, (role, value) in enumerate([*metadata.items(), ('other', 'Technical prose')]):
            lines.append(dict(id=case, note='notes.txt', line_index=j, line=value,
                              source_input_sha256='a'*64, label_status='agent-reviewed',
                              roles=[role], role_disputed=False, mixed_scope=False, uncertainty=False, conflict=False))
    protocol = freeze_protocol(sources, gold, lines, classifier_model_sha256=MODEL, execution_manifest=EXECUTION)
    frozen = digest(protocol)
    predictions = [{**{k: row[k] for k in ('id', 'note', 'line_index', 'source_input_sha256')},
                    'freeze_sha256': frozen, 'model_sha256': MODEL, 'roles': row['roles']}
                   for row in lines]
    arms = {}
    for name in ('baseline', 'fixed', 'assisted'):
        rows = []
        for label in gold:
            output = dict(metadata={f: '' for f in FIELDS}, ready=False,
                          review_reasons=['source review required'], mappings={}, write_plan={})
            row = dict(id=label['id'], source_input_sha256='a'*64,
                       freeze_sha256=frozen, output=output)
            if name == 'assisted':
                row['proposals'] = [dict(field=f, value=v, note='notes.txt', line_index=i,
                                         start=0, end=len(v), quote=v)
                                    for i, (f, v) in enumerate(label['metadata'].items())]
            rows.append(row)
        arms[name] = dict(application_commit=BASELINE_COMMIT if name == 'baseline' else FIXED_COMMIT,
                          model_sha256=MODEL, rows=rows)
    return protocol, gold, lines, arms, predictions


class StudyEvaluationTests(unittest.TestCase):
    def test_valid_three_arm_evaluation_still_requires_independent_review(self):
        result = evaluate(*fixture())
        self.assertTrue(result['quantitative_gates_pass'])
        self.assertFalse(result['integration_approved'])
        self.assertEqual(result['review_cohort']['paired_improvement'], 1)
        self.assertEqual(result['background']['lines'], 200)
        self.assertEqual(result['roles']['venue']['supporting_recordings'], 200)

    def test_insufficient_independent_support_blocks(self):
        result = evaluate(*fixture(3))
        self.assertFalse(result['gates']['role_precision_and_support'])
        self.assertFalse(result['gates']['adjudicated_test_support'])

    def test_readiness_values_and_review_reasons_cannot_change(self):
        for field, value in [('ready', True), ('review_reasons', []), ('mappings', {'file': 'wrong'}),
                             ('write_plan', {'write': True}), ('metadata', {f: 'invented' for f in FIELDS})]:
            data = fixture(3)
            data[3]['assisted']['rows'][0]['output'][field] = value
            self.assertFalse(evaluate(*data)['gates']['assistance_parity'], field)

    def test_background_denominator_counts_all_labeled_background_lines(self):
        data = fixture(200)
        for i in range(3):
            data[4][i*5+4]['roles'] = ['artist']
        result = evaluate(*data)
        self.assertEqual(result['background']['false_positive_rate'], .015)
        self.assertEqual(result['background']['affected_recordings'], 3)
        self.assertFalse(result['gates']['background_false_positive_rate'])

    def test_abstentions_and_missing_outputs_remain_in_cohort(self):
        data = fixture(3)
        data[3]['assisted']['rows'].pop()
        data[4].pop()
        result = evaluate(*data)
        self.assertEqual(result['review_cohort']['count'], 3)
        self.assertEqual(result['review_cohort']['proposed_metadata_exact'], 2)
        self.assertFalse(result['gates']['complete_candidate_outputs'])

    def test_no_candidate_defined_review_cohort_or_mutated_gold(self):
        data = fixture(3)
        data[0]['review_cohort'] = []
        with self.assertRaisesRegex(ValueError, 'inconsistent'):
            evaluate(*data)
        data = fixture(3)
        data[1][0]['metadata']['venue'] = 'Changed'
        with self.assertRaisesRegex(ValueError, 'gold changed'):
            evaluate(*data)

    def test_duplicate_predictions_stale_models_and_pre_freeze_rows_rejected(self):
        for mutation in ('duplicate', 'model', 'freeze'):
            data = fixture(3)
            if mutation == 'duplicate':
                data[4].append(copy.deepcopy(data[4][0]))
            elif mutation == 'model':
                data[4][0]['model_sha256'] = 'c'*64
            else:
                data[3]['fixed']['rows'][0]['freeze_sha256'] = ''
            with self.assertRaises(ValueError):
                evaluate(*data)

    def test_unicode_spans_and_nonabstaining_role_evidence_required(self):
        for mutation in ('offset', 'quote', 'abstention'):
            data = fixture(3)
            p = data[3]['assisted']['rows'][0]['proposals'][2]
            if mutation == 'offset':
                p['end'] += 1  # UTF-8 byte length is not codepoint length.
            elif mutation == 'quote':
                p['quote'] = 'Hall'
            else:
                data[4][2].update(roles=[], abstention='threshold_margin')
            result = evaluate(*data)
            self.assertFalse(result['gates']['source_backed_proposals'])

    def test_new_accepted_wrong_metadata_blocks_even_if_gold_allows_ready(self):
        data = fixture(3)
        data[1][0]['review_required'] = False
        data = list(data)
        data[0] = freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)
        for arm in data[3].values():
            for row in arm['rows']:
                row['freeze_sha256'] = digest(data[0])
        for row in data[4]:
            row['freeze_sha256'] = digest(data[0])
        data[3]['fixed']['rows'][0]['output']['ready'] = True
        result = evaluate(*data)
        self.assertEqual(result['new_false_ready_ids'], [])
        self.assertEqual(result['new_material_acceptance_ids'], ['case-0'])
        self.assertFalse(result['gates']['no_new_material_acceptance'])

    def test_gold_scope_flags_measure_ineligible_proposals_without_changing_outputs(self):
        for flag in ('mixed_scope', 'uncertainty', 'conflict'):
            data = list(fixture(3))
            data[2][0][flag] = True
            data[0] = freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)
            for arm in data[3].values():
                for row in arm['rows']:
                    row['freeze_sha256'] = digest(data[0])
            for row in data[4]:
                row['freeze_sha256'] = digest(data[0])
            before = copy.deepcopy(data[3])
            result = evaluate(*data)
            self.assertFalse(result['gates']['source_eligible_metadata_proposals'])
            self.assertEqual(result['source_ineligible_proposals'][0]['flags'], [flag])
            self.assertEqual(result['review_cohort']['proposed_metadata_exact'], 2)
            self.assertEqual(data[3], before)

    def test_unknown_line_gold_is_explicit_and_reference_shortfall_blocks(self):
        data = list(fixture(3))
        for row in data[2][:5]:
            row.update(label_status='unresolved', roles=[], role_disputed=True)
        data[0] = freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)
        for arm in data[3].values():
            for row in arm['rows']:
                row['freeze_sha256'] = digest(data[0])
        for row in data[4]:
            row['freeze_sha256'] = digest(data[0])
        result = evaluate(*data)
        self.assertEqual(result['line_coverage']['disputed_lines'], 5)
        self.assertEqual(result['reference_coverage']['reviewed_nonblank_fraction'], 2/3)
        self.assertFalse(result['gates']['reviewed_nonblank_reference_coverage'])
        self.assertEqual(result['background']['lines'], 2)
        data[2][0]['roles'] = ['artist']
        with self.assertRaisesRegex(ValueError, 'empty roles'):
            freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)

    def test_valid_line_gold_survives_unresolved_recording_gold(self):
        data = list(fixture(3))
        data[1][0]['label_status'] = 'unresolved'
        data[0] = freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)
        for arm in data[3].values():
            for row in arm['rows']:
                row['freeze_sha256'] = digest(data[0])
        for row in data[4]:
            row['freeze_sha256'] = digest(data[0])
        result = evaluate(*data)
        self.assertEqual(result['arms']['fixed']['eligible'], 2)
        self.assertEqual(result['roles']['artist']['supporting_recordings'], 3)
        self.assertEqual(result['reference_coverage']['eligible_recording_gold'], 2)

    def test_unlabeled_lines_cannot_masquerade_as_complete_gold(self):
        data = fixture(3)
        data[2][0]['roles'] = []
        with self.assertRaisesRegex(ValueError, 'explicitly disputed'):
            freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)

    def test_unreviewed_or_incomplete_gold_cannot_freeze(self):
        data = fixture(3)
        with self.assertRaisesRegex(ValueError, 'coverage'):
            freeze_protocol(data[0]['sources'], data[1], data[2][:-1], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)
        data[1][0]['label_status'] = 'draft'
        with self.assertRaisesRegex(ValueError, 'independently reviewed'):
            freeze_protocol(data[0]['sources'], data[1], data[2], classifier_model_sha256=MODEL, execution_manifest=EXECUTION)


if __name__ == '__main__':
    unittest.main()
