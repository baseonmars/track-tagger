"""Local-only llama.cpp structured extraction adapter."""
import hashlib
import json
import time
import os
import tempfile
import subprocess
import urllib.parse
import urllib.request
from pathlib import Path
from .hosted import canonical, request_schema
from .scoring import validate

def sample_process(pid):
    fields=subprocess.check_output(['ps','-p',str(pid),'-o','time=','-o','rss='],text=True).split()
    parts=[float(x) for x in fields[0].split(':')]
    return sum(x*60**i for i,x in enumerate(reversed(parts))),int(fields[1])*1024

class LocalFailure(ValueError):
    def __init__(self, kind, result):
        super().__init__(kind)
        self.result = result

class LocalAdapter:
    def __init__(self, config):
        self.config = dict(config)
        identity = self.config.get('execution_identity', {})
        if any(not identity.get(k) for k in ('model_sha256','runtime_commit','backend','context_tokens')):
            raise ValueError('model hash, runtime commit, backend and context are required')
        u = urllib.parse.urlparse(self.config['endpoint'])
        if u.scheme != 'http' or u.hostname not in ('127.0.0.1', 'localhost', '::1'):
            raise ValueError('local adapter requires a loopback HTTP endpoint')
        self.cache = Path(self.config['cache_dir'])
        self.cache.mkdir(parents=True, exist_ok=True)
    def parse(self, case, prompt, phase):
        from .guards import candidate_guard
        candidate_guard(case,phase)
        safe = {'notes':[{k:n[k] for k in ('name','text')} for n in case['notes']], 'files':case['files']}
        policy=self.config.get('output_token_policy', 'fixed')
        if policy == 'adaptive-v1':
            max_tokens=min(16384,max(8192,2048+220*len(case['files'])))
        elif policy == 'fixed':
            max_tokens=self.config.get('max_tokens',8192)
        else:
            raise ValueError('unknown output token policy')
        settings={'temperature':0, 'max_tokens':max_tokens, 'seed':self.config.get('seed',42)}
        payload={'model':self.config['model'], 'messages':[{'role':'system','content':prompt},{'role':'user','content':canonical(safe)}], **settings,
                 'response_format':{'type':'json_schema','json_schema':{'name':'concert_notes','strict':True,'schema':request_schema(self.config,case)}}, 'chat_template_kwargs':{'enable_thinking':False}}
        body=canonical(payload).encode()
        # UTF-8 bytes conservatively bound tokens; include template overhead.
        prompt_bytes=len(prompt.encode('utf-8')) + len(canonical(safe).encode('utf-8')) + 512
        if prompt_bytes + settings['max_tokens'] > int(self.config['execution_identity']['context_tokens']):
            raise LocalFailure('context_limit',{'failure':{'kind':'context_limit'},'usage':{},'latency_seconds':0})
        key=hashlib.sha256((phase+'\0'+canonical(self.config['execution_identity'])+'\0').encode()+body).hexdigest(); path=self.cache/(key+'.json')
        if path.exists():
            value=json.loads(path.read_text()); value['cache_hit']=True; return value
        before_cpu,before_rss=sample_process(self.config['server_pid']) if self.config.get('server_pid') else (None,None)
        start=time.monotonic(); req=urllib.request.Request(self.config['endpoint'].rstrip('/')+'/v1/chat/completions',body,{'Content-Type':'application/json'})
        timeout=self.config.get('timeout',600)
        if self.config.get('deadline_epoch'):
            timeout=min(timeout,self.config['deadline_epoch']-time.time())
            if timeout<=0: raise TimeoutError('local run deadline elapsed')
        with urllib.request.urlopen(req,timeout=timeout) as response: raw=json.load(response)
        resources={}
        if before_cpu is not None:
            after_cpu,after_rss=sample_process(self.config['server_pid'])
            resources={'server_cpu_seconds':max(0,after_cpu-before_cpu),'sampled_rss_bytes':max(before_rss,after_rss)}
        def fail(kind):
            failure_path=self.cache/(key+'.failure.json')
            with tempfile.NamedTemporaryFile(mode='w',dir=self.cache,delete=False) as stream:
                stream.write(canonical(raw)); temporary=stream.name
            os.replace(temporary,failure_path)
            raise LocalFailure(kind,{'failure':{'kind':kind,'raw_response_path':str(failure_path)},'resources':resources,'usage':raw.get('usage',{}),'latency_seconds':time.monotonic()-start,'actual_model':raw.get('model'),'settings':{**settings,'output_token_policy':policy,'execution_identity':self.config['execution_identity']}})
        try:
            choice=raw['choices'][0]
            if choice.get('finish_reason') != 'stop': fail('incomplete_response')
            output=json.loads(choice['message']['content']); titles=output['titles']; mappings=output['mappings']
            if len({x['id'] for x in titles})!=len(titles) or len({x['path'] for x in mappings})!=len(mappings): fail('duplicate_identifiers')
            output['titles']={x['id']:x['title'] for x in titles}
            output['mappings']={x['path']:{k:x[k] for k in ('disc','number','title')} for x in mappings}
            validate(output)
        except LocalFailure: raise
        except (ValueError,KeyError,IndexError,TypeError): fail('malformed_response')
        result={'resources':resources,'output':output,'usage':raw.get('usage',{}),'cache_hit':False,'latency_seconds':time.monotonic()-start,'actual_model':raw.get('model'),'settings':{**settings,'output_token_policy':policy,'execution_identity':self.config['execution_identity']}}
        with tempfile.NamedTemporaryFile(mode='w',dir=self.cache,delete=False) as stream:
            stream.write(canonical(result)); temporary=stream.name
        os.replace(temporary,path)
        return result
