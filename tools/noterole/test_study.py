import importlib.util,json,pathlib,subprocess,tempfile,unittest
from study import fit,select,ROLES,PROTOCOL
ROOT=pathlib.Path(__file__).resolve().parents[2]
@unittest.skipUnless(importlib.util.find_spec('sklearn'),'offline training dependency unavailable')
class StudyTests(unittest.TestCase):
 def test_train_candidates_then_validation_freeze(self):
  with tempfile.TemporaryDirectory() as td:
   out=pathlib.Path(td)/'models';exe=pathlib.Path(td)/'extractor';subprocess.run(['go','build','-o',str(exe),'./cmd/noterole-features'],cwd=ROOT,check=True)
   def rows(split):return [dict(id=split+str(record),note='notes.txt',line_index=i,group=split+str(record),source_input_sha256='a'*64,split=split,line='synthetic '+role,prev='',next='',roles=[role])for record in range(12)for i,role in enumerate(ROLES)]
   train=rows('train');val=rows('validation');m=fit(train,exe,out)
   self.assertEqual([c['C']for c in m['candidates']],PROTOCOL['C'])
   with self.assertRaises(ValueError):select([{**r,'split':'test'}for r in val],exe,out)
   f=select(val,exe,out);self.assertFalse(f['test_inspected']);self.assertEqual(len(f['selection_results']),3)
   payload=''.join(json.dumps({k:r[k]for k in ('line','prev','next')})+'\n'for r in val)
   emitted=[json.loads(s)for s in subprocess.check_output([str(exe),'--model',str(out/'model.json')],input=payload,text=True).splitlines()]
   chosen=next(c for c in f['selection_results']if c['C']==f['selected_C'])
   for role in ROLES:self.assertEqual(sum(role in r['roles']for r in emitted),chosen['roles'][role]['predicted_positive_lines'])
   self.assertTrue(set(f['enabled_metadata_roles'])<=set(PROTOCOL['metadata_roles']))
   with self.assertRaises(ValueError):select(val,exe,out)
