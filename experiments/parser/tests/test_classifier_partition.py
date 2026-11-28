import unittest
from experiments.parser.classifier_partition import partition

class ClassifierPartitionTest(unittest.TestCase):
    def test_excludes_overlap_unknown_template_and_group_closure(self):
        cases=[{'id':'t1','group':'g1','split':'train'},{'id':'t2','group':'g1','split':'train'},
               {'id':'t3','group':'g2','split':'train'},{'id':'v1','group':'g3','split':'validation'},
               {'id':'v2','group':'g4','split':'validation'},{'id':'x1','group':'g5','split':'test'},
               {'id':'x2','group':'g6','split':'test'}]
        us={'t1':'shared','t2':'other','t3':'','v1':'shared','v2':'test-user','x1':'test-user','x2':'test-template'}
        excluded,unknown=partition(cases,us,{'t2','x2'})
        self.assertEqual(set(excluded),{'t1','t2','t3','v2'})
        self.assertIn('uploader_in_validation_or_test',excluded['t2'])
        self.assertIn('verified_test_template_family',excluded['t2'])
        self.assertIn('missing_uploader',excluded['t3'])
        self.assertEqual(unknown,[])
        self.assertFalse({'x1','x2'} & set(excluded))
