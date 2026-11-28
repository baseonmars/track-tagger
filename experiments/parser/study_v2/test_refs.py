import copy
import unittest
import tempfile
import json
from pathlib import Path
from .refs import references,rehydrate,read
from .labels import source_hash

class ReferenceTests(unittest.TestCase):
    def fixtures(self):
        case={'id':'fixture','group':'g','split':'train','notes':[{'name':'note.txt','text':'Artist\nNext\n'}],'files':[]}
        row={'id':'fixture','group':'g','split':'train','note':'note.txt','line_index':0,'line':'Artist','prev':'','next':'Next','roles':['artist'],'label_status':'source-authored','source_input_sha256':source_hash(case),'uncertainty':False,'conflict':False,'mixed_scope':False,'role_disputed':False,'spans':[]}
        return case,row
    def test_export_has_no_source_lines_and_replays_context(self):
        case,row=self.fixtures();refs=references([row])
        self.assertNotIn('line',refs[0]);self.assertNotIn('next',refs[0]);actual=rehydrate(refs,[case])[0]
        self.assertEqual(actual['line'],'Artist');self.assertEqual(actual['next'],'Next')
    def test_source_context_or_split_change_rejected(self):
        case,row=self.fixtures();refs=references([row]);case['notes'][0]['text']='Artist\nChanged\n'
        with self.assertRaisesRegex(ValueError,'source membership'):rehydrate(refs,[case])
        case,row=self.fixtures();refs=references([row]);refs[0]['split']='test'
        with self.assertRaisesRegex(ValueError,'source membership'):rehydrate(refs,[case])
    def test_jsonl_embedded_unicode_separator_is_not_record_boundary(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'rows.jsonl';path.write_text(json.dumps({'line':'a\u2028b'},ensure_ascii=False)+'\n')
            self.assertEqual(read(path),[{'line':'a\u2028b'}])
    def test_duplicate_references_rejected(self):
        case,row=self.fixtures()
        with self.assertRaisesRegex(ValueError,'duplicate'):references([row,copy.deepcopy(row)])

if __name__=='__main__':unittest.main()
