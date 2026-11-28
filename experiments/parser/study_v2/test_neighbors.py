import json
import unittest
from .neighbors import audit

def case(id,split,text):return {'id':id,'split':split,'files':['01.flac'],'notes':[{'name':'notes.txt','text':text}]}
class NeighborTests(unittest.TestCase):
    def test_cross_split_and_old_leads_without_source_text(self):
        text='Source transferred using distinctive microphone and recorder with compilation footer'
        result=audit([case('train','train',text),case('test','test',text)],[case('old','train',text)])
        pairs=result['pairs'];self.assertTrue(any(p['right_origin']=='old' for p in pairs));self.assertTrue(any({p['left_id'],p['right_id']}=={'train','test'}for p in pairs))
        self.assertNotIn(text,json.dumps(result));self.assertTrue(all(p['jaccard']==1 for p in pairs))
    def test_duplicate_and_bounds(self):
        c=case('same','train','a b c d e')
        with self.assertRaises(ValueError):audit([c],[c])
        with self.assertRaises(ValueError):audit([c],[],0)
    def test_same_split_not_audit_lead(self):
        self.assertEqual(audit([case('a','train','a b c d e'),case('b','train','a b c d e')],[])['pairs'],[])
if __name__=='__main__':unittest.main()
