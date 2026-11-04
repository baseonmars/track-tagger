"""Disposable artifact score parity; no credentials, corpus, or sklearn needed."""
import json, math, pathlib, subprocess, tempfile, unittest
ROOT=pathlib.Path(__file__).resolve().parents[2]
ROLES=['artist','date','venue','city','title','heading','event','continuation','technical_provenance','other','blank']
class Parity(unittest.TestCase):
 def test_unicode_sparse_and_logits(self):
  with tempfile.TemporaryDirectory() as td:
   exe=str(pathlib.Path(td)/'features')
   subprocess.run(['go','build','-o',exe,'./cmd/noterole-features'],cwd=ROOT,check=True)
   rows=[{'line':'Élan artist 名','prev':'Previous','next':'01 Song >'},{'line':'artist artist','prev':'','next':''}]
   payload=''.join(json.dumps(r)+'\n' for r in rows)
   features=[json.loads(s)['features'] for s in subprocess.check_output([exe],input=payload,text=True).splitlines()]
   vocab=sorted(set().union(*(f.keys() for f in features)))
   heads=[dict(role=r,weights=[((i%7)-3)*.125 for i in range(len(vocab))],intercept=.2,threshold=.6,margin=.05) for r in ROLES]
   artifact=dict(schema_version=1,feature_version='noterole-unicode-v1',model_id='disposable-parity',training_sha256='a'*64,vocabulary=vocab,heads=heads)
   path=pathlib.Path(td)/'model.json';path.write_text(json.dumps(artifact))
   results=[json.loads(s) for s in subprocess.check_output([exe,'--model',str(path)],input=payload,text=True).splitlines()]
   for f,result in zip(features,results):
    for h in heads:
     z=h['intercept']
     for k in sorted(f): z+=f[k]*h['weights'][vocab.index(k)]
     self.assertAlmostEqual(result['scores'][h['role']],1/(1+math.exp(-z)),places=14)
if __name__=='__main__':unittest.main()
