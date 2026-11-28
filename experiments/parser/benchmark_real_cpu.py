"""Run the preselected real validation CPU benchmark cases sequentially."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

def main():
    p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--selection',type=Path,required=True);p.add_argument('--frozen',type=Path,required=True);p.add_argument('--prompt',type=Path,required=True);p.add_argument('--model',choices=('0.6b','1.7b','1.7b-q4'),required=True);p.add_argument('--backend',choices=('cpu','metal'),default='cpu');args=p.parse_args()
    selected=json.loads(args.selection.read_text())
    if hashlib.sha256(args.frozen.read_bytes()).hexdigest()!=selected['source_freeze_sha256']: raise SystemExit('selection source freeze changed')
    for index,case_id in enumerate(selected['ids']):
        output=args.root/'run'/('benchmark-real-'+args.backend+'-'+args.model+'-'+str(index+1)+'.json')
        command=[sys.executable,'-m','experiments.parser.benchmark_local','--root',str(args.root),'--cases',str(args.frozen),'--id',case_id,'--backend',args.backend,'--model',args.model,'--prompt',str(args.prompt),'--adaptive-output','--evidence-note-enum','--output',str(output)]
        subprocess.run(command,check=True)
if __name__=='__main__':main()
