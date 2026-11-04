import unittest
import tempfile
from pathlib import Path
from experiments.parser.classifier_refs import references,rehydrate,require_external,verify_public_source
from experiments.parser.source_review import source_hash

class ClassifierRefsTest(unittest.TestCase):
    def test_rejects_other_git_worktree(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'.git').write_text('gitdir: /somewhere')
            with self.assertRaises(ValueError):require_external(root/'nested'/'notes.jsonl')
    def test_public_source_identity_and_content(self):
        case={'id':'a','group':'g','split':'train','notes':[{'name':'n','text':'one'}],'files':[]}
        manifest={'cases':[{'id':'a','group':'g','split':'train','input_sha256':source_hash(case)}]}
        verify_public_source([case],manifest)
        with self.assertRaises(ValueError):verify_public_source([],manifest)
        with self.assertRaises(ValueError):verify_public_source([{**case,'split':'test'}],manifest)
        with self.assertRaises(ValueError):verify_public_source([{**case,'notes':[{'name':'n','text':'changed'}]}],manifest)
    def test_replay_checks_context_partition_and_duplicates(self):
        c={'id':'a','group':'g','split':'train','notes':[{'name':'n.txt','text':'head\nArtist: Band\nend\n'}],'files':[]}
        row={'id':'a','group':'g','split':'train','note':'n.txt','line_index':1,'label':'artist','line':'Artist: Band','prev':'head','next':'end'}
        refs=references([row],[c],[{'id':'a','label_status':'draft'}],{'train':'snapshot-sha'})
        self.assertEqual(rehydrate(refs,[c],{'exclusions':{}},refs),[row])
        bad={**c,'notes':[{'name':'n.txt','text':'head\nArtist: Band\nchanged\n'}]}
        with self.assertRaises(ValueError):rehydrate(refs,[bad],{'exclusions':{}},refs)
        with self.assertRaises(ValueError):rehydrate(refs,[c],{'exclusions':{'a':['reason']}},refs)
        changed=[{**refs[0],'label':'date'}]
        with self.assertRaises(ValueError):rehydrate(changed,[c],{'exclusions':{}},refs)
        changed=[{**refs[0],'annotation_status':'disputed'}]
        with self.assertRaises(ValueError):rehydrate(changed,[c],{'exclusions':{}},refs)
        with self.assertRaises(ValueError):rehydrate([],[c],{'exclusions':{}},refs)
        with self.assertRaises(ValueError):rehydrate(refs+refs,[c],{'exclusions':{}},refs+refs)

class ClassifierReplayCLITest(unittest.TestCase):
    def test_public_train_and_test_with_frozen_manifests(self):
        import importlib.util
        if not importlib.util.find_spec('sklearn'): self.skipTest('optional classifier dependencies unavailable')
        import hashlib, json, subprocess, sys
        with tempfile.TemporaryDirectory() as d:
            root=Path(d); source=root/'source.jsonl'; partition=root/'partition.json'; source_manifest=root/'source-manifest.json'
            cases=[{'id':'a','group':'ga','split':'train','notes':[{'name':'n','text':'Artist Band'}],'files':[]},
                   {'id':'b','group':'gb','split':'train','notes':[{'name':'n','text':'Date 2001'}],'files':[]},
                   {'id':'c','group':'gc','split':'test','notes':[{'name':'n','text':'Artist Other'}],'files':[]}]
            source.write_text(''.join(json.dumps(c)+'\n' for c in cases));partition.write_text(json.dumps({'source_sha256':'private-source','exclusions':{}}))
            source_manifest.write_text(json.dumps({'cases':[{'id':c['id'],'group':c['group'],'split':c['split'],'input_sha256':source_hash(c)} for c in cases]}))
            sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
            def mkrefs(split,path,manifest):
                rows=[]
                for c in cases:
                    if (c['split']=='test') != (split=='test'):continue
                    text=c['notes'][0]['text'];label='date' if c['id']=='b' else 'artist'
                    rows.extend(references([{'id':c['id'],'group':c['group'],'split':c['split'],'note':'n','line_index':0,'label':label,'line':text,'prev':'','next':''}],[c],[{'id':c['id'],'label_status':'agent-reviewed' if c['split']=='test' else 'draft'}],{c['split']:'annotation'}))
                path.write_text(''.join(json.dumps(r)+'\n' for r in rows));manifest.write_text(json.dumps({'stage':split,'refs_sha256':sha(path),'source_sha256':'private-source','source_manifest_sha256':sha(source_manifest),'partition_sha256':sha(partition)}))
            train=root/'train.jsonl';train_manifest=root/'train-manifest.json';mkrefs('train-validation',train,train_manifest)
            modeldir=root/'fit';base=[sys.executable,'-m','experiments.parser.classifier_refs']
            def cmd(stage,refs,manifest,out):return base+[stage,'--frozen',str(source),'--partition',str(partition),'--refs',str(refs),'--refs-manifest',str(manifest),'--source-manifest',str(source_manifest),'--output',str(out)]
            subprocess.run(cmd('train-replay',train,train_manifest,modeldir),check=True,capture_output=True)
            self.assertTrue((modeldir/'training-freeze.json').exists())
            test=root/'test.jsonl';test_manifest=root/'test-manifest.json';mkrefs('test',test,test_manifest)
            frozen=json.loads((modeldir/'training-freeze.json').read_text());test_meta=json.loads(test_manifest.read_text());test_meta.update(training_refs_sha256=frozen['train_refs_sha256'],model_sha256=frozen['model_sha256']);test_manifest.write_text(json.dumps(test_meta))
            result=root/'result.json';subprocess.run(cmd('evaluate-replay',test,test_manifest,result)+['--model',str(modeldir/'classifier.joblib'),'--training-freeze',str(modeldir/'training-freeze.json')],check=True,capture_output=True)
            self.assertEqual(json.loads(result.read_text())['lines'],1)
            wrong_freeze=root/'wrong-freeze.json';wrong_freeze.write_text(json.dumps({**frozen,'source_manifest_sha256':'wrong'}))
            self.assertNotEqual(subprocess.run(cmd('evaluate-replay',test,test_manifest,root/'bad-source')+['--model',str(modeldir/'classifier.joblib'),'--training-freeze',str(wrong_freeze)],capture_output=True).returncode,0)
            missing_model={k:v for k,v in test_meta.items() if k!='model_sha256'};test_manifest.write_text(json.dumps(missing_model))
            self.assertNotEqual(subprocess.run(cmd('evaluate-replay',test,test_manifest,root/'missing-model')+['--model',str(modeldir/'classifier.joblib'),'--training-freeze',str(modeldir/'training-freeze.json')],capture_output=True).returncode,0)
            wrong={**test_meta,'training_refs_sha256':'wrong'};test_manifest.write_text(json.dumps(wrong))
            self.assertNotEqual(subprocess.run(cmd('evaluate-replay',test,test_manifest,root/'bad-eval')+['--model',str(modeldir/'classifier.joblib'),'--training-freeze',str(modeldir/'training-freeze.json')],capture_output=True).returncode,0)
            bad=json.loads(train_manifest.read_text());bad['stage']='test';train_manifest.write_text(json.dumps(bad))
            self.assertNotEqual(subprocess.run(cmd('train-replay',train,train_manifest,root/'bad'),capture_output=True).returncode,0)
            bad['stage']='train-validation';bad['refs_sha256']='wrong';train_manifest.write_text(json.dumps(bad))
            self.assertNotEqual(subprocess.run(cmd('train-replay',train,train_manifest,root/'bad2'),capture_output=True).returncode,0)
            bad['refs_sha256']=sha(train);bad['partition_sha256']='wrong';train_manifest.write_text(json.dumps(bad))
            self.assertNotEqual(subprocess.run(cmd('train-replay',train,train_manifest,root/'bad3'),capture_output=True).returncode,0)
