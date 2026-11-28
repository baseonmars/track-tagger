import unittest
from experiments.parser.real_line_classifier import examples
from experiments.parser.source_review import source_hash

class RealLineTest(unittest.TestCase):
    def test_only_unambiguous_source_quotes_become_examples(self):
        case={'id':'a','group':'g','split':'train','notes':[{'name':'note.txt','text':'Artist: The Band\n01. A Song\nRepeated\nRepeated\nUnknown prose\n'}],'files':['01.flac']}
        draft={'id':'a','label_status':'draft','source_sha256':source_hash(case),'oracle':{'artist':'The Band','titles':{'1:1':'A Song'},'date':'2001-01-01','venue':'Somewhere','evidence':[{'field':'artist','note':'note.txt','quote':'The Band'},{'field':'titles.1:1','note':'note.txt','quote':'A Song'},{'field':'date','note':'note.txt','quote':'Repeated'},{'field':'venue','note':'note.txt','quote':'Unknown\nprose'}]}}
        rows,coverage=examples([case],[draft],[])
        self.assertEqual([r['label'] for r in rows],['artist','title'])
        self.assertEqual(coverage['missing_or_ambiguous_quote'],1)
        self.assertEqual(coverage['empty_or_multiline_quote'],1)
        self.assertNotIn('Unknown prose',[r['line'] for r in rows])
    def test_requires_review_and_matching_source(self):
        case={'id':'b','group':'h','split':'validation','notes':[{'name':'n','text':'Date: 2001-01-01'}],'files':[]}
        review={'id':'b','label_status':'agent-reviewed','annotation_review':{'source_sha256':source_hash(case)},'oracle':{'date':'2001-01-01','evidence':[{'field':'date','note':'n','quote':'2001-01-01'}]}}
        rows,counts=examples([case],[],[review]); self.assertEqual(rows[0]['label'],'date')
        review['annotation_review']['source_sha256']='wrong'; rows,counts=examples([case],[],[review]);self.assertEqual(rows,[]);self.assertEqual(counts['stale_or_invalid_reviews'],1)
    def test_citation_to_empty_field_is_not_positive(self):
        case={'id':'a','group':'g','split':'train','notes':[{'name':'n','text':'2001-01-01'}],'files':[]}
        draft={'id':'a','label_status':'draft','source_sha256':source_hash(case),'oracle':{'date':'','evidence':[{'field':'date','note':'n','quote':'2001-01-01'}]}}
        rows,counts=examples([case],[draft],[]);self.assertEqual(rows,[]);self.assertEqual(counts['uncited_oracle_field'],1)
        with self.assertRaises(ValueError): examples([case],[draft,draft],[])
    def test_rejects_cross_split_groups(self):
        with self.assertRaises(ValueError): examples([{'id':'a','group':'g','split':'train'}, {'id':'b','group':'g','split':'test'}],[],[])
