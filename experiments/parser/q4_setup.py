"""Reproduce the optional Q4_K_M model from pinned official BF16, never Q8."""
import argparse
import hashlib
import json
import os
import signal
from pathlib import Path
import shutil
import subprocess
import time
from .local_setup import COMMIT, DERIVED_MODELS, sha256
from .pilot_local import remaining_seconds
from .path_guard import require_external

REVISION='70d244cc86ccca08cf5af4e1e306ecf908b1ad5e'
SHARDS={'model-00001-of-00002.safetensors':'169ad53ec313c3a34b06c0809216e4fc072cce444a5d4ff2b59690d064130ed5','model-00002-of-00002.safetensors':'912becff8d60672aa8628ef08c05898d9adf17c2ad4ae3caf99b065622fdeff9'}

def run_bounded(cmd, timeout):
    process=subprocess.Popen(cmd,start_new_session=True)
    try:
        code=process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid,signal.SIGKILL);process.wait();raise
    if code:raise subprocess.CalledProcessError(code,cmd)

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--python',type=Path,required=True);p.add_argument('--cmake',default='cmake');p.add_argument('--jobs',type=int,default=6);a=p.parse_args()
    root=require_external(a.root);source=root/'llama.cpp';budget=remaining_seconds(root/'run/session.json',1);deadline=time.monotonic()+budget
    if shutil.disk_usage(root).free<12*1024**3:raise SystemExit('at least 12 GiB free required')
    if subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD'],text=True).strip()!=COMMIT or subprocess.check_output(['git','-C',str(source),'status','--porcelain','--untracked-files=no'],text=True).strip():raise SystemExit('runtime source is not clean pinned commit')
    hf=root/'models/qwen3-1.7b-bf16-source';bf=root/'models/qwen3-1.7b-derived-bf16.gguf';out=root/'models/qwen3-1.7b-q4'/DERIVED_MODELS['1.7b-q4'][2];build=root/'q4-build'
    if out.exists() or bf.exists():raise SystemExit('derived model already exists; choose fresh external root')
    def run(cmd):
        left=deadline-time.monotonic()
        if left<=0:raise TimeoutError('Q4 setup deadline elapsed')
        run_bounded(cmd,left)
    run([str(a.python),'-c','from huggingface_hub import snapshot_download; import sys; snapshot_download("Qwen/Qwen3-1.7B",revision=sys.argv[1],local_dir=sys.argv[2])',REVISION,str(hf)])
    for name,expected in SHARDS.items():
        if sha256(hf/name)!=expected:raise ValueError('official BF16 shard hash mismatch')
    run([a.cmake,'-S',str(source),'-B',str(build),'-DGGML_METAL=OFF','-DCMAKE_BUILD_TYPE=Release'])
    run([a.cmake,'--build',str(build),'--target','llama-quantize','-j',str(a.jobs)])
    run([str(a.python),str(source/'convert_hf_to_gguf.py'),str(hf),'--outfile',str(bf),'--outtype','bf16'])
    out.parent.mkdir(parents=True,exist_ok=True);run([str(build/'bin/llama-quantize'),str(bf),str(out),'Q4_K_M',str(a.jobs)])
    actual=sha256(out)
    if actual!=DERIVED_MODELS['1.7b-q4'][3]:raise ValueError('derived Q4 differs from recorded experiment hash')
    manifest={'source_repository':'https://huggingface.co/Qwen/Qwen3-1.7B','source_revision':REVISION,'source_shard_sha256':SHARDS,'runtime_commit':COMMIT,'q4_sha256':actual,'q4_bytes':out.stat().st_size,'bf16_sha256':sha256(bf),'license':'Apache-2.0','note':'derived from official BF16 via pinned conversion and Q4_K_M; not requantized Q8'}
    (root/'run/q4-reproduced-provenance.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
if __name__=='__main__':main()
