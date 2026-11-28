"""Cold and warm local llama.cpp timing on a caller-selected external JSONL case."""
import argparse
import hashlib
import json
import os
import re
import math
import statistics
from pathlib import Path
import signal
import socket
import subprocess
import time
import urllib.request
from .hosted import canonical, request_schema
from .runner import PROMPT, read_cases
from .local_setup import COMMIT, MODELS, DERIVED_MODELS, sha256
MODEL_SPECS={**MODELS,**DERIVED_MODELS}
from .pilot_local import remaining_seconds

def sample_process(pid):
    fields=subprocess.check_output(['ps','-p',str(pid),'-o','time=','-o','rss='],text=True).split()
    parts=[float(x) for x in fields[0].split(':')]
    cpu=sum(x * 60**i for i,x in enumerate(reversed(parts)))
    return cpu,int(fields[1])*1024

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--cases',type=Path,required=True);p.add_argument('--id',required=True);p.add_argument('--backend',choices=('cpu','metal'),required=True);p.add_argument('--model',choices=tuple(MODEL_SPECS),default='1.7b');p.add_argument('--output',type=Path,required=True);p.add_argument('--prompt',type=Path);p.add_argument('--adaptive-output',action='store_true');p.add_argument('--evidence-note-enum',action='store_true');p.add_argument('--deadline-seconds',type=int,default=600);p.add_argument('--port',type=int,default=18882);args=p.parse_args()
    if args.output.exists(): raise SystemExit('refusing to overwrite output')
    cases=[c for c in read_cases(args.cases) if c['id']==args.id]
    if len(cases)!=1: raise SystemExit('expected one matching case')
    c=cases[0];root=args.root.resolve();build=root/'llama.cpp'/('build' if args.backend=='cpu' else 'build-metal')/'bin';binary=build/'llama-server'
    # Official filenames use an uppercase B after the decimal size.
    model=root/'models'/('qwen3-'+args.model)/MODEL_SPECS[args.model][2]
    source=root/'llama.cpp'
    commit=subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD'],text=True).strip()
    if commit!=COMMIT or subprocess.check_output(['git','-C',str(source),'status','--porcelain','--untracked-files=no'],text=True).strip(): raise SystemExit('runtime checkout is not the clean pinned commit')
    model_hash=sha256(model)
    if model_hash != MODEL_SPECS[args.model][3]: raise SystemExit('model hash mismatch')
    binary_hash=sha256(binary)
    dylib_hashes={p.name:sha256(p) for p in sorted(build.glob('*.dylib')) if not p.is_symlink()}
    prompt=args.prompt.read_text() if args.prompt else PROMPT
    safe={'notes':[{k:n[k] for k in ('name','text')} for n in c['notes']],'files':c['files']}
    max_tokens=min(16384,max(8192,2048+220*len(c['files']))) if args.adaptive_output else 8192
    if len(prompt.encode())+len(canonical(safe).encode())+512+max_tokens>32768: raise SystemExit('case exceeds conservative context bound')
    payload={'model':'qwen3-'+args.model if args.model.endswith('-q4') else 'qwen3-'+args.model+'-q8','messages':[{'role':'system','content':prompt},{'role':'user','content':canonical(safe)}],'temperature':0,'max_tokens':max_tokens,'seed':42,'response_format':{'type':'json_schema','json_schema':{'name':'concert_notes','strict':True,'schema':request_schema({'evidence_note_enum':args.evidence_note_enum},c)}},'chat_template_kwargs':{'enable_thinking':False}}
    body=canonical(payload).encode();cmd=[str(binary),'-m',str(model),'--host','127.0.0.1','--port',str(args.port),'-c','32768','-np','1','--no-context-shift','--reasoning','off','-t','6','-lv','4']
    cmd+=['-ngl','99'] if args.backend=='metal' else ['-ngl','0','--no-kv-offload','--no-op-offload']
    with socket.socket() as probe:
        if probe.connect_ex(('127.0.0.1',args.port)) == 0: raise SystemExit('benchmark port already in use')
    args.output.parent.mkdir(parents=True,exist_ok=True); log_path=args.output.with_suffix('.server.log'); env=os.environ.copy();env['DYLD_LIBRARY_PATH']=str(build)
    budget=remaining_seconds(root/'run/session.json',args.deadline_seconds/3600)
    started=time.monotonic();deadline=started+budget
    with log_path.open('x') as log:
        proc=subprocess.Popen(['/usr/bin/time','-lp','/usr/bin/env','DYLD_LIBRARY_PATH='+str(build)]+cmd,stdout=log,stderr=subprocess.STDOUT,env=env,start_new_session=True)
        try:
            while time.monotonic()<deadline:
                if proc.poll() is not None: raise RuntimeError('server exited before readiness')
                try:
                    with urllib.request.urlopen('http://127.0.0.1:%d/health'%args.port,timeout=1) as response:
                        if response.status==200 and 'server_on_li' in log_path.read_text(): break
                except OSError: time.sleep(.1)
            else: raise TimeoutError('server readiness deadline')
            startup=time.monotonic()-started;samples=[];responses=[]
            children=subprocess.check_output(['pgrep','-P',str(proc.pid)],text=True).split()
            if len(children)!=1: raise RuntimeError('could not identify owned server')
            server_pid=int(children[0])
            for i in range(3):
                left=deadline-time.monotonic()
                if left<=0: raise TimeoutError('benchmark deadline')
                cpu_before,rss_before=sample_process(server_pid)
                before=time.monotonic();req=urllib.request.Request('http://127.0.0.1:%d/v1/chat/completions'%args.port,body,{'Content-Type':'application/json'})
                with urllib.request.urlopen(req,timeout=left) as response: result=json.load(response)
                cpu_after,rss_after=sample_process(server_pid)
                choice=result['choices'][0];responses.append({'call':i+1,'choice':choice,'usage':result.get('usage',{})});samples.append({'server_cpu_seconds':max(0,cpu_after-cpu_before),'sampled_rss_bytes':max(rss_before,rss_after),'call':i+1,'latency_seconds':time.monotonic()-before,'usage':result.get('usage',{}),'finish_reason':choice.get('finish_reason'),'content_sha256':hashlib.sha256(choice['message']['content'].encode()).hexdigest()})
        finally:
            children=subprocess.run(['pgrep','-P',str(proc.pid)],capture_output=True,text=True).stdout.split()
            for child in children:
                try: os.kill(int(child),signal.SIGINT)
                except ProcessLookupError: pass
            try: proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid,signal.SIGKILL);proc.wait()
            # A child that ignored graceful shutdown must not survive the wrapper.
            try: os.killpg(proc.pid,signal.SIGKILL)
            except ProcessLookupError: pass
    log_text=log_path.read_text()
    offload=re.search(r'offloaded (\d+)/(\d+) layers to GPU',log_text)
    if args.backend=='metal' and (not offload or int(offload.group(1)) == 0): raise RuntimeError('Metal offload not demonstrated; inspect resource log')
    if args.backend=='cpu' and offload and int(offload.group(1)) != 0: raise RuntimeError('CPU benchmark unexpectedly offloaded layers')
    identity={'runtime_commit':commit,'model_sha256':model_hash,'binary_sha256':binary_hash,'dylib_sha256':dylib_hashes,'request_sha256':hashlib.sha256(body).hexdigest(),'case_sha256':hashlib.sha256(canonical(safe).encode()).hexdigest(),'gpu_layers_offloaded':int(offload.group(1)) if offload else 0,'settings':{'context_tokens':32768,'max_tokens':max_tokens,'temperature':0,'seed':42,'threads':6,'context_shift':False}}
    latencies=sorted(x['latency_seconds'] for x in samples);cpu_samples=sorted(x['server_cpu_seconds'] for x in samples)
    percentile=lambda xs,q:xs[max(0,math.ceil(len(xs)*q)-1)]
    summary={'n':len(samples),'latency_p50_seconds':statistics.median(latencies),'latency_p95_seconds':percentile(latencies,.95),'server_cpu_p50_seconds':statistics.median(cpu_samples),'server_cpu_p95_seconds':percentile(cpu_samples,.95),'max_sampled_rss_bytes':max(x['sampled_rss_bytes'] for x in samples)}
    responses_path=args.output.with_suffix('.responses.jsonl')
    with responses_path.open('x') as stream:
        for response in responses: stream.write(json.dumps(response,ensure_ascii=False)+'\n')
    result={'responses_path':str(responses_path),'summary':summary,'execution_identity':identity,'backend':args.backend,'model':args.model,'case_id':args.id,'server_startup_seconds':startup,'samples':samples,'resource_log':str(log_path),'note':'first call follows server start; subsequent calls use server prompt cache. CPU and Metal can generate different outputs.'}
    args.output.write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
if __name__=='__main__':main()
