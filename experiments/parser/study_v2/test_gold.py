import copy
import unittest
from .gold import promote
from .labels import source_hash

class GoldTests(unittest.TestCase):
    def fixtures(self):
        c={'id':'a','group':'g','split':'validation','notes':[{'name':'x.txt','text':'Artist\n\nOther\n'}],'files':[]}
        segments=[{'note':'x.txt','start_line':i,'end_line':i+1,'roles':[role],'uncertainty':False,'conflict':False,'mixed_scope':False,'role_disputed':False,'spans':[]}for i,role in [(0,'artist'),(2,'other')]]
        annotation={'artist':'Artist','date':'','venue':'','city':'','titles':{},'mappings':{},'review':True,'unresolved':['date/venue unknown'],'evidence':[{'field':'artist','note':'x.txt','quote':'Artist'}],'segments':segments}
        inputs=[{**c,'annotation_draft':copy.deepcopy(annotation)}]
        row={'id':'a','source_input_sha256':source_hash(c),'annotation':{**annotation,'verdict':'accepted'},'label_status':'source-reviewed'}
        return[c],inputs,[row]
    def test_explicit_source_blank_convention_retains_nonblank_roles(self):
        cases,inputs,rows=self.fixtures();recording,flat,audit=promote(cases,inputs,rows)
        self.assertEqual(recording[0]['label_status'],'agent-reviewed');self.assertEqual([r['roles']for r in flat],[['artist'],['blank'],['other']]);self.assertTrue(audit[0]['derived_blank_conventions'])
    def test_missing_annotation_is_unknown_not_background(self):
        cases,inputs,rows=self.fixtures();rows[0]['annotation']=None
        recording,flat,_=promote(cases,inputs,rows)
        self.assertEqual(recording[0]['label_status'],'unresolved')
        self.assertTrue(all(r['roles']==[]and r['role_disputed']for r in flat if r['line'].strip()))
    def test_invalid_recording_oracle_does_not_erase_reviewed_line_gold(self):
        cases,inputs,rows=self.fixtures();rows[0]['annotation']['date']='2026-99-99';rows[0]['annotation']['verdict']='corrected'
        recording,flat,_=promote(cases,inputs,rows)
        self.assertEqual(recording[0]['label_status'],'unresolved');self.assertTrue(all(r['label_status']=='agent-reviewed'for r in flat))
    def test_accepted_verdict_cannot_change_line_truth_silently(self):
        cases,inputs,rows=self.fixtures();rows[0]['annotation']['segments']=copy.deepcopy(rows[0]['annotation']['segments']);rows[0]['annotation']['segments'][0]['roles']=['city']
        _,flat,audit=promote(cases,inputs,rows)
        self.assertTrue(audit[0]['line_reference_issue']);self.assertTrue(flat[0]['role_disputed'])
    def test_foreign_review_source_or_duplicate_membership_rejected(self):
        cases,inputs,rows=self.fixtures();inputs[0]['group']='other'
        with self.assertRaisesRegex(ValueError,'frozen membership'):promote(cases,inputs,rows)
        cases,inputs,rows=self.fixtures();inputs.append(copy.deepcopy(inputs[0]))
        with self.assertRaisesRegex(ValueError,'membership'):promote(cases,inputs,rows)

if __name__=='__main__':unittest.main()
