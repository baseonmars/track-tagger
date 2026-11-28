import importlib.util,json,pathlib,subprocess,tempfile,unittest
from train import train,ROLES
ROOT=pathlib.Path(__file__).resolve().parents[2]
@unittest.skipUnless(importlib.util.find_spec('sklearn'),'offline training dependency unavailable')
class Training(unittest.TestCase):
 def test_full_multilabel_pipeline_and_test_refusal(self):
  with tempfile.TemporaryDirectory() as td:
   exe=str(pathlib.Path(td)/'extractor');out=str(pathlib.Path(td)/'model.json')
   subprocess.run(['go','build','-o',exe,'./cmd/noterole-features'],cwd=ROOT,check=True)
   rows=[dict(id=str(i),group=split+str(i),source_input_sha256='a'*64,split=split,line='example '+role,prev='',next='',roles=[role]) for split in ('train','validation') for i,role in enumerate(ROLES)]
   rows.append(dict(id='multi',group='multi',source_input_sha256='b'*64,split='train',line='artist venue combined',prev='',next='',roles=['artist','venue']))
   summary=train(rows,exe,out,'synthetic-training-test')
   self.assertEqual(summary['eligible_lines'],23)
   artifact=json.loads(pathlib.Path(out).read_text());self.assertEqual(len(artifact['heads']),11)
   # The exported trained artifact must be loadable by the real Go scorer.
   actual=json.loads(subprocess.check_output([exe,'--model',out],input=json.dumps(dict(line='example artist',prev='',next=''))+'\n',text=True))
   self.assertEqual(len(actual['scores']),11)
   with self.assertRaises(ValueError):train(rows+[dict(split='test')],exe,out,'forbidden')
