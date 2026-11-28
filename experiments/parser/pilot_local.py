"""Run one local candidate against an immutable, reviewed validation pilot."""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import signal
import socket
import subprocess
import time
import urllib.request
from .local import LocalAdapter
from .local_setup import COMMIT, MODELS, DERIVED_MODELS, sha256
MODEL_SPECS={**MODELS,**DERIVED_MODELS}
from .runner import read_cases, run

def remaining_seconds(session_path, requested_hours, now=None):
    session=json.loads(Path(session_path).read_text())
    remaining=min(requested_hours*3600, float(session['inference_deadline_epoch'])-(time.time() if now is None else now))
    if remaining<=0: raise ValueError('shared inference deadline elapsed')
    return remaining

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--frozen-pilot',type=Path,required=True);p.add_argument('--adapter',type=Path,required=True);p.add_argument('--model',choices=tuple(MODEL_SPECS),required=True);p.add_argument('--backend',choices=('cpu','metal'),required=True);p.add_argument('--prompt',type=Path,required=True);p.add_argument('--test-common',action='store_true');p.add_argument('--synthetic-common',action='store_true');p.add_argument('--test-full-remaining',type=Path);p.add_argument('--port',type=int,default=18881);p.add_argument('--hours',type=float,default=1);args=p.parse_args()
    root=args.root.resolve();budget=remaining_seconds(root/'run/session.json',args.hours);cases=read_cases(args.frozen_pilot)
    if sum(bool(x) for x in (args.test_common,args.test_full_remaining,args.synthetic_common))>1: raise SystemExit('choose only one test mode')
    expected_count,expected_split=(200,'test') if args.test_full_remaining else (40,'test') if (args.test_common or args.synthetic_common) else (30,'validation')
    allowed=('synthetic',) if args.synthetic_common else ('agent-reviewed','adjudicated','disputed','unresolved') if (args.test_common or args.test_full_remaining) else ('agent-reviewed','adjudicated')
    if len(cases)!=expected_count or any(c.get('split')!=expected_split or c.get('label_status') not in allowed or (not args.synthetic_common and not c.get('_labels_frozen_sha256')) or (args.synthetic_common and c.get('provenance',{}).get('kind')!='synthetic') for c in cases): raise SystemExit('requires frozen reviewed cases in the expected split')
    if args.test_full_remaining:
        prior=read_cases(args.test_full_remaining);done={r['id'] for r in prior}
        if len(prior)!=40 or len(done)!=40 or not done.issubset({c['id'] for c in cases}): raise SystemExit('requires complete unique common40 predictions')
        cases=[c for c in cases if c['id'] not in done]
    source=root/'llama.cpp';commit=subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD'],text=True).strip()
    if commit!=COMMIT or subprocess.check_output(['git','-C',str(source),'status','--porcelain','--untracked-files=no'],text=True).strip(): raise SystemExit('runtime checkout is not the clean pinned commit')
    build=source/('build' if args.backend=='cpu' else 'build-metal')/'bin';binary=build/'llama-server';model=root/'models'/('qwen3-'+args.model)/MODEL_SPECS[args.model][2]
    if sha256(model)!=MODEL_SPECS[args.model][3]: raise SystemExit('model SHA256 mismatch')
    with socket.socket() as probe:
        if probe.connect_ex(('127.0.0.1',args.port))==0: raise SystemExit('server port already in use')
    phase=('frozen-full200-remaining-' if args.test_full_remaining else 'frozen-synthetic40-' if args.synthetic_common else 'frozen-common40-' if args.test_common else 'frozen-pilot-')+args.model+'-'+args.backend+'-r2';run_dir=root/'run';run_dir.mkdir(parents=True,exist_ok=True);output=run_dir/(phase+'.jsonl');log_path=run_dir/(phase+'.server.log')
    if output.exists() or log_path.exists(): raise SystemExit('phase already exists; preserve the earlier artifact')
    command=[str(binary),'-m',str(model),'--host','127.0.0.1','--port',str(args.port),'-c','32768','-np','1','--no-context-shift','--reasoning','off','-t','6','-lv','4']
    command+=['-ngl','99'] if args.backend=='metal' else ['-ngl','0','--no-kv-offload','--no-op-offload']
    env=os.environ.copy();env['DYLD_LIBRARY_PATH']=str(build);deadline=time.monotonic()+budget
    with log_path.open('x') as log:
        proc=subprocess.Popen(command,stdout=log,stderr=subprocess.STDOUT,env=env,start_new_session=True)
        try:
            while time.monotonic()<deadline:
                if proc.poll() is not None: raise RuntimeError('server exited before readiness')
                try:
                    with urllib.request.urlopen('http://127.0.0.1:%d/health'%args.port,timeout=1) as response:
                        if response.status==200 and 'server_on_li' in log_path.read_text():break
                except OSError: time.sleep(.1)
            else: raise TimeoutError('server readiness deadline')
            text=log_path.read_text()
            offload=re.search(r'offloaded (\d+)/(\d+) layers to GPU',text)
            if args.backend=='metal' and (not offload or int(offload.group(1))==0): raise RuntimeError('GPU layer offload not demonstrated')
            if args.backend=='cpu' and offload and int(offload.group(1))!=0: raise RuntimeError('CPU backend unexpectedly offloaded layers')
            identity={'model_sha256':MODEL_SPECS[args.model][3],'runtime_commit':commit,'binary_sha256':sha256(binary),'backend':args.backend,'context_tokens':32768,'frozen_pilot_sha256':hashlib.sha256(args.frozen_pilot.read_bytes()).hexdigest()}
            config={'model':'qwen3-'+args.model if args.model.endswith('-q4') else 'qwen3-'+args.model+'-q8','endpoint':'http://127.0.0.1:%d'%args.port,'cache_dir':str(root/('cache-'+phase)),'execution_identity':identity,'output_token_policy':'adaptive-v1','identifier_executable':str(args.adapter),'server_pid':proc.pid,'evidence_note_enum':True,'timeout':600,'deadline_epoch':time.time()+max(0,deadline-time.monotonic())}
            remaining=max(0,(deadline-time.monotonic())/3600);print(run(cases,LocalAdapter(config),output,phase,prompt=args.prompt.read_text(),hours=remaining))
        finally:
            try:os.killpg(proc.pid,signal.SIGINT)
            except ProcessLookupError:pass
            try:proc.wait(timeout=20)
            except subprocess.TimeoutExpired:os.killpg(proc.pid,signal.SIGKILL);proc.wait()
if __name__=='__main__':main()
