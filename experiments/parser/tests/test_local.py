import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from experiments.parser.local import LocalAdapter

class Response:
    def __enter__(self): return self
    def __exit__(self,*args): pass
    def read(self,*args):
        value={'artist':'A','date':'','venue':'','city':'','titles':[{'id':'1:1','title':'T'}],'mappings':[{'path':'01.flac','disc':1,'number':1,'title':'T'}],'review':False,'unresolved':[],'evidence':[]}
        return json.dumps({'model':'test','choices':[{'finish_reason':'stop','message':{'content':json.dumps(value)}}]}).encode()

class LocalTest(unittest.TestCase):
    def test_loopback_and_cache(self):
        with tempfile.TemporaryDirectory() as d:
            config={'endpoint':'http://127.0.0.1:9000','cache_dir':d,'model':'test','execution_identity':{'model_sha256':'sha','runtime_commit':'commit','backend':'cpu','context_tokens':16384}}
            adapter=LocalAdapter(config); case={'notes':[{'name':'a.txt','text':'secret','encoding':'utf-8'}],'files':['01.flac'],'oracle':{'secret':'expected'}}
            with patch('urllib.request.urlopen', return_value=Response()) as call:
                first=adapter.parse(case,'prompt','phase'); second=adapter.parse(case,'prompt','phase')
                self.assertEqual(call.call_count,1); body=json.loads(call.call_args.args[0].data)
                self.assertNotIn('oracle',body['messages'][1]['content']); self.assertFalse(first['cache_hit']); self.assertTrue(second['cache_hit'])
                self.assertEqual(first['output']['titles'],{'1:1':'T'})
    def test_adaptive_output_and_context_limit(self):
        with tempfile.TemporaryDirectory() as d:
            config={'endpoint':'http://127.0.0.1:9000','cache_dir':d,'model':'test','output_token_policy':'adaptive-v1','execution_identity':{'model_sha256':'sha','runtime_commit':'commit','backend':'cpu','context_tokens':32768}}
            adapter=LocalAdapter(config); case={'notes':[{'name':'n','text':'x'}],'files':['%03d.flac'%i for i in range(60)]}
            with patch('urllib.request.urlopen', return_value=Response()) as call:
                result=adapter.parse(case,'p','phase');body=json.loads(call.call_args.args[0].data)
                self.assertEqual(body['max_tokens'],15248); self.assertEqual(result['settings']['max_tokens'],15248)
            case['notes'][0]['text']='x'*33000
            with self.assertRaises(ValueError): adapter.parse(case,'p','other')
    def test_incomplete_response_retained_externally(self):
        class Incomplete(Response):
            def read(self,*args): return json.dumps({'model':'x','usage':{'completion_tokens':3},'choices':[{'finish_reason':'length','message':{'content':'partial'}}]}).encode()
        with tempfile.TemporaryDirectory() as d:
            config={'endpoint':'http://127.0.0.1:9000','cache_dir':d,'model':'test','execution_identity':{'model_sha256':'sha','runtime_commit':'commit','backend':'cpu','context_tokens':16384}}
            case={'notes':[{'name':'n','text':'x'}],'files':['1.flac']}
            with patch('urllib.request.urlopen',return_value=Incomplete()):
                with self.assertRaises(ValueError) as err: LocalAdapter(config).parse(case,'p','phase')
            self.assertEqual(err.exception.result['failure']['kind'],'incomplete_response')
            self.assertTrue(Path(err.exception.result['failure']['raw_response_path']).is_file())
    def test_remote_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(ValueError): LocalAdapter({'endpoint':'http://example.com','cache_dir':d,'model':'x','execution_identity':{'model_sha256':'sha','runtime_commit':'commit','backend':'cpu','context_tokens':16384}})
