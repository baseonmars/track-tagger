import json
import unittest
from experiments.parser.study_v2_evaluation import evaluate
from experiments.parser.study_v2_public import export_summary
from experiments.parser.tests.test_study_v2_evaluation import fixture


class PublicExportTests(unittest.TestCase):
    def test_raw_text_paths_errors_and_unrecognized_fields_are_not_exported(self):
        result = evaluate(*fixture(3))
        result.update(raw_notes='PRIVATE_NOTE', provider_response='PRIVATE_RESPONSE',
                      executable='/private/home/bin', exception='PRIVATE_ERROR')
        result['arms']['baseline']['raw_values'] = {'artist':'PRIVATE_ARTIST'}
        result['roles']['artist']['source_example'] = 'PRIVATE_SOURCE'
        result['parity_failure_ids'] = ['PRIVATE_CASE_IDENTIFIER']
        encoded = json.dumps(export_summary(result))
        self.assertNotIn('PRIVATE_', encoded)
        self.assertNotIn('/private/', encoded)
        self.assertEqual(len(json.loads(encoded)['failure_index'][0]['case_sha256']), 64)

    def test_malformed_public_values_and_auto_approval_rejected(self):
        result = evaluate(*fixture(3))
        result['roles']['artist']['precision'] = 'PRIVATE_SECRET'
        with self.assertRaises(ValueError):export_summary(result)
        result = evaluate(*fixture(3));result['integration_approved'] = True
        with self.assertRaises(ValueError):export_summary(result)
        result = evaluate(*fixture(3));result['background']['false_positive_rate'] = float('nan')
        with self.assertRaises(ValueError):export_summary(result)

    def test_numeric_results_preserved_exactly(self):
        result = evaluate(*fixture())
        public = export_summary(result)
        self.assertEqual(public['arms'], result['arms'])
        self.assertEqual(public['gates'], result['gates'])
        self.assertEqual(public['review_cohort'], result['review_cohort'])
        self.assertTrue(public['quantitative_gates_pass'])
        self.assertFalse(public['integration_approved'])


if __name__ == '__main__':unittest.main()
