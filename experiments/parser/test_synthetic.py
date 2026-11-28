import unittest
from experiments.parser.synthetic import FAMILIES, make_case

class SyntheticTests(unittest.TestCase):
    def test_family_splits_do_not_overlap(self):
        by_split = {}
        for seed in range(80):
            c = make_case(seed)
            by_split.setdefault(c['split'], set()).add(c['group'])
            self.assertEqual(len(c['line_labels']), len(c['notes'][0]['text'].splitlines()))
            self.assertEqual(set(c['files']), set(c['oracle']['mappings']))
            self.assertEqual(c['provenance']['label_status'], 'generated_not_agent_reviewed')
            if 'conflicting_dates' in c['features']:
                dates = [line.removeprefix('Date: ') for line,label in zip(c['notes'][0]['text'].splitlines(),c['line_labels']) if label == 'date']
                self.assertEqual(len(set(dates)), 2)
        self.assertEqual(sum(map(len, by_split.values())), len(FAMILIES))
    def test_scenarios_are_rendered_from_truth(self):
        collision = make_case(403695061)
        dates = [line.removeprefix('Date: ') for line,label in zip(collision['notes'][0]['text'].splitlines(), collision['line_labels']) if label == 'date']
        self.assertEqual(len(set(dates)), 2)
        duplicate = make_case(10)
        self.assertIn('1:2', duplicate['oracle']['unresolved'])
        self.assertTrue(duplicate['oracle']['review'])
        multi = make_case(30)
        self.assertIn('2:1', multi['oracle']['titles'])
        encore = make_case(50)
        self.assertIn('1:5', encore['oracle']['titles'])
        self.assertIn('Encore:', encore['notes'][0]['text'])
        wrapped = make_case(70)
        self.assertIn('and the Long Road', wrapped['oracle']['titles']['1:4'])
        self.assertTrue(wrapped['oracle']['review'])

if __name__ == '__main__': unittest.main()
