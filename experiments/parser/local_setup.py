#!/usr/bin/env python3
"""Explicitly provision pinned local experiment dependencies on macOS."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
from .path_guard import require_external

COMMIT = '149f5641b931aaf33697d70f9abc90c3e5300247'
MODELS = {
    '0.6b': ('Qwen/Qwen3-0.6B-GGUF','23749fefcc72300e3a2ad315e1317431b06b590a','Qwen3-0.6B-Q8_0.gguf','9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031'),
    '1.7b': ('Qwen/Qwen3-1.7B-GGUF','90862c4b9d2787eaed51d12237eafdfe7c5f6077','Qwen3-1.7B-Q8_0.gguf','061b54daade076b5d3362dac252678d17da8c68f07560be70818cace6590cb1a'),
}
DERIVED_MODELS = {'1.7b-q4': ('derived-from-official-bf16', '70d244cc86ccca08cf5af4e1e306ecf908b1ad5e', 'Qwen3-1.7B-derived-Q4_K_M.gguf', 'cf7f7ce724f5d403b15a8f5763fc5fcafe0220fa202f34f879511e9675870bb9')}

def sha256(path):
    h=hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda:f.read(1<<20),b''): h.update(block)
    return h.hexdigest()

def main():
    p=argparse.ArgumentParser(); p.add_argument('--root',type=Path,required=True); p.add_argument('--cmake',default='cmake'); p.add_argument('--jobs',type=int,default=6); args=p.parse_args()
    if platform.system()!='Darwin': raise SystemExit('This setup targets macOS only')
    root=require_external(args.root); root.mkdir(parents=True,exist_ok=True); source=root/'llama.cpp'
    if not source.exists():
        subprocess.run(['git','clone','--no-checkout','https://github.com/ggml-org/llama.cpp.git',str(source)],check=True)
        subprocess.run(['git','-C',str(source),'checkout','--detach',COMMIT],check=True)
    actual=subprocess.check_output(['git','-C',str(source),'rev-parse','HEAD'],text=True).strip()
    if actual!=COMMIT: raise SystemExit('Existing checkout differs from pinned commit; choose another external root')
    if subprocess.check_output(['git','-C',str(source),'status','--porcelain','--untracked-files=no'],text=True).strip():
        raise SystemExit('Runtime checkout has tracked edits; choose a clean external root')
    for backend in ('cpu','metal'):
        build=source/('build' if backend=='cpu' else 'build-metal')
        cache=build/'CMakeCache.txt'
        if cache.exists() and ('CMAKE_HOME_DIRECTORY:INTERNAL='+str(source)) not in cache.read_text():
            raise SystemExit('Existing CMake build refers to another source path; use a fresh root')
        subprocess.run([args.cmake,'-S',str(source),'-B',str(build),'-DGGML_METAL='+('OFF' if backend=='cpu' else 'ON'),'-DLLAMA_BUILD_SERVER=ON','-DCMAKE_BUILD_TYPE=Release'],check=True)
        subprocess.run([args.cmake,'--build',str(build),'--target','llama-server','-j',str(args.jobs)],check=True)
    manifest={'runtime':{'repository':'https://github.com/ggml-org/llama.cpp','commit':COMMIT,'platform':'macOS','builds':['cpu: GGML_METAL=OFF','metal: GGML_METAL=ON']},'models':{}}
    from huggingface_hub import hf_hub_download
    for name,(repo,revision,filename,expected) in MODELS.items():
        path=Path(hf_hub_download(repo_id=repo,revision=revision,filename=filename,local_dir=str(root/'models'/('qwen3-'+name))))
        if sha256(path)!=expected: raise SystemExit('Model SHA256 mismatch: '+filename)
        manifest['models'][name]={'repository':'https://huggingface.co/'+repo,'revision':revision,'file':str(path),'sha256':expected,'license':'Apache-2.0'}
    (root/'local-setup-manifest.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
    print('Pinned setup verified:',root)
if __name__=='__main__': main()
