import copy
import hashlib
import unittest
from ..hosted import canonical
from .freeze_corpus import check_proposed_binding
from .labels import source_hash

class BindingTests(unittest.TestCase):
    def fixtures(self):
        c={'id':'a','group':'g','split':'test','notes':[{'name':'x.txt','text':'Artist\n'}],'files':['t01.flac']};c['source_input_sha256']=source_hash(c)
        row={k:c[k]for k in ('id','group','split','source_input_sha256','files')};row['notes']=[{'name':'x.txt','normalized_text_sha256':hashlib.sha256(b'Artist\n').hexdigest()}]
        manifest={'cases':[row]};digest=hashlib.sha256(canonical(manifest).encode()).hexdigest();manifest['manifest_sha256']=digest;c['_source_manifest_sha256']=digest
        return {'manifest_sha256':digest,'cases':[c]},manifest
    def test_exact_membership_valid(self):
        p,m=self.fixtures();self.assertEqual(check_proposed_binding(p,m),m['manifest_sha256'])
    def test_edited_split_group_duplicate_or_top_hash_rejected(self):
        for mutation in ('split','group','duplicate','top_hash'):
            p,m=self.fixtures()
            if mutation in ('split','group'):p['cases'][0][mutation]='changed'
            elif mutation=='duplicate':p['cases'].append(copy.deepcopy(p['cases'][0]))
            else:p['manifest_sha256']='0'*64
            with self.assertRaises(ValueError):check_proposed_binding(p,m)

if __name__=='__main__':unittest.main()
