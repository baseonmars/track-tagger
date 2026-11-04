"""Second-pass source annotation review; never accepts candidate predictions.

Output and audit contain original source text and must remain outside Git.
Agent-reviewed labels are machine source reviews, not human certification.
"""
import argparse
import concurrent.futures
import hashlib
import json
import os
import re
from pathlib import Path
import time

from .annotations import REVIEW_PROMPT, review_schema
from .budget import BudgetExceeded, Ledger
from .hosted import Adapter, canonical
from .runner import read_cases
from .scoring import FIELDS, evidence_score, valid_date, validate

REVIEW_PROMPT = REVIEW_PROMPT + ''' Distinguish ambiguous recording evidence from an unresolved annotation: an oracle that correctly leaves unsupported fields empty, leaves mappings unresolved and sets review=true can be accepted or corrected. Use disputed/unresolved only when you cannot establish the correct expected safe response. Do not exclude ambiguous recordings from gold labels merely because their correct response requires review. A ready oracle requires nonempty artist, date and venue, a nonempty all-FLAC audio inventory, and complete unambiguous matching titles; otherwise set review=true.'''
PROTOCOL_VERSION = 1
VERDICTS = {'accepted', 'corrected', 'disputed', 'unresolved'}


def sha(value):
    return hashlib.sha256(canonical(value).encode()).hexdigest()


def source_hash(case):
    return sha({'notes':case['notes'],'files':case['files']})


def validate_review(case, draft, response):
    """Return promotion blockers. Passing requires a full independent model review."""
    issues = []
    if not isinstance(response, dict) or set(response) != {'verdict','oracle','reasons'}:
        return ['invalid-review-schema']
    if not isinstance(response['verdict'],str) or response['verdict'] not in VERDICTS:
        issues.append('invalid-verdict')
    if not isinstance(response['reasons'],list) or any(not isinstance(r,str) for r in response['reasons']):
        issues.append('invalid-review-reasons')
    oracle = response['oracle']
    if not isinstance(oracle,dict) or set(oracle) != FIELDS:
        return issues + ['incomplete-oracle-schema']
    try:
        validate(oracle)
    except (ValueError, TypeError, AttributeError):
        return issues + ['invalid-oracle']
    if any(not isinstance(key,str) or not re.fullmatch(r'[1-9]\d*:[1-9]\d*',key) for key in oracle['titles']):
        issues.append('invalid-oracle-title-identifiers')
    if oracle['date'] and not valid_date(oracle['date']):
        issues.append('invalid-calendar-date')
    evidence = evidence_score(oracle,case['notes'])
    if evidence['invalid_quotes']:
        issues.append('invalid-source-quotes')
    if evidence['unsupported_populated_fields']:
        issues.append('missing-source-citations')
    files = case['files']
    if not oracle['review']:
        for field in ('artist','date','venue'):
            if not oracle[field]:
                issues.append('missing-'+field+'-requires-review')
        if not files:
            issues.append('empty-audio-inventory-requires-review')
    if set(oracle['mappings']) != set(files) or len(files) != len(set(files)):
        issues.append('incomplete-or-foreign-file-inventory')
    populated_ids = []
    for mapping in oracle['mappings'].values():
        disc,number,title = mapping['disc'],mapping['number'],mapping['title']
        if not title:
            if disc or number:
                issues.append('partly-unresolved-mapping')
            if not oracle['review']:
                issues.append('unmapped-file-requires-review')
        else:
            key = f'{disc}:{number}'
            populated_ids.append(key)
            if not disc or not number or oracle['titles'].get(key) != title:
                issues.append('mapping-title-inconsistency')
    if len(populated_ids) != len(set(populated_ids)) and not oracle['review']:
        issues.append('duplicate-mapping-identifiers-require-review')
    if set(oracle['titles']) - set(populated_ids) and not oracle['review']:
        issues.append('unmapped-titles-require-review')
    if oracle['unresolved'] and not oracle['review']:
        issues.append('unresolved-evidence-requires-review')
    if any(Path(name).suffix.lower() != '.flac' for name in files) and not oracle['review']:
        issues.append('non-flac-package-requires-review')
    if response['verdict'] == 'accepted':
        semantic_fields = FIELDS - {'evidence'}
        if any(oracle[k] != draft['oracle'].get(k) for k in semantic_fields):
            issues.append('accepted-verdict-changes-draft')
    return sorted(set(issues))


def append(path, row):
    path = Path(path)
    path.parent.mkdir(parents=True,exist_ok=True)
    with path.open('a') as stream:
        stream.write(canonical(row)+'\n')
        stream.flush()
        os.fsync(stream.fileno())


def review_annotations(cases, draft_rows, adapter, output, audit, hours=3, phase='annotation-sol-source-review-v1', allow_synthetic_training_smoke=False):
    """Review frozen validation/test source annotations using a shared cost ledger.

    Resumes only records with identical source/draft/config/prompt fingerprints.
    Disputed, unresolved, malformed, or incomplete reviews never become accuracy
    eligible. Every returned provider response is retained in the external audit.
    """
    config = adapter.config
    if config.get('annotation_review') is not True:
        raise ValueError('source reviewer requires explicit annotation_review adapter mode')
    if config.get('model') != 'gpt-6.1-sol':
        raise ValueError('review requires the exact verified model gpt-6.1-sol')
    if config.get('prices_verified') is not True or float(config['input_per_million']) != 2 or float(config['output_per_million']) != 10:
        raise ValueError('review requires verified Sol input/output prices 2/10 per million')
    drafts = {row['id']:row for row in draft_rows}
    if len(drafts) != len(draft_rows):
        raise ValueError('duplicate draft identifiers')
    previous = {row['id']:row for row in read_cases(output)} if Path(output).exists() else {}
    deadline = time.monotonic() + hours*3600
    completed, promoted, errors = 0,0,0
    for case in cases:
        synthetic_smoke = allow_synthetic_training_smoke and case.get('split') == 'train' and case.get('provenance',{}).get('kind') == 'synthetic' and phase.startswith('annotation-smoke-')
        if case.get('split') not in ('validation','test') and not synthetic_smoke:
            raise ValueError('source review requires frozen validation/test packages')
        if case['id'] not in drafts:
            raise ValueError('missing independent source annotation draft')
        draft = drafts[case['id']]
        if draft['source_sha256'] != source_hash(case):
            raise ValueError('stale source annotation draft')
        # A named draft can only be an annotation, never parser output.
        if draft.get('label_status') != 'draft' or not isinstance(draft.get('oracle'),dict):
            raise ValueError('review accepts annotation drafts only')
        implementation=sha({name:hashlib.sha256(Path(__file__).with_name(name).read_bytes()).hexdigest() for name in ('source_review.py','scoring.py','annotations.py')})
        fingerprint = sha({'implementation_sha256':implementation,'protocol':PROTOCOL_VERSION,'source':source_hash(case),'draft':sha(draft),
                           'config':config,'prompt':REVIEW_PROMPT,'phase':phase})
        if case['id'] in previous:
            if previous[case['id']]['annotation_review']['fingerprint'] != fingerprint:
                raise ValueError('review inputs changed; use a new output path')
            continue
        if time.monotonic() >= deadline:
            break
        base = {'id':case['id'],'protocol_version':PROTOCOL_VERSION,'fingerprint':fingerprint,
                'source_sha256':source_hash(case),'draft_sha256':sha(draft),
                'prompt_sha256':hashlib.sha256(REVIEW_PROMPT.encode()).hexdigest(),
                'config_sha256':sha(config),'requested_model':config['model'],'phase':phase,
                'reviewed_at_epoch':time.time()}
        try:
            result = adapter.parse({**case,'annotation_draft':draft['oracle']},REVIEW_PROMPT,phase)
        except BudgetExceeded:
            break
        except Exception as exc:
            append(audit,{**base,'error':type(exc).__name__,
                          'hosted_failure':getattr(exc,'result',None)})
            errors += 1
            continue
        response = result['output']
        issues = validate_review(case,draft,response)
        verdict = response.get('verdict') if isinstance(response,dict) else None
        status = 'agent-reviewed' if isinstance(verdict,str) and verdict in {'accepted','corrected'} and not issues else 'disputed' if verdict == 'disputed' or issues else 'unresolved'
        review_record = {**base,'reviewer_model':result.get('actual_model'),'verdict':verdict,
                         'promotion_blockers':issues,'usage':result.get('usage'),
                         'settings':result.get('settings'),'cache_hit':result.get('cache_hit'),
                         'response_sha256':sha(response),'not_human_certified':True}
        append(audit,{**review_record,'reviewer_response':response,'draft_annotation':draft})
        row = {**case,'oracle':response.get('oracle') if isinstance(response,dict) else None,
               'label_status':status,'annotation_review':review_record}
        append(output,row)
        completed += 1
        promoted += status == 'agent-reviewed'
    return {'new_reviews':completed,'new_agent_reviewed':promoted,'request_errors':errors,
            'not_human_certified':True}


def review_annotations_parallel(cases, draft_rows, adapter, output, audit, workers=4, hours=3):
    """Bounded independent requests with per-case recovery and one merge writer."""
    if not 1 <= workers <= 4:
        raise ValueError('review workers must be 1..4')
    if len({case['id'] for case in cases}) != len(cases):
        raise ValueError('duplicate source identifiers')
    eligible_drafts = [row for row in draft_rows if row.get('label_status') == 'draft']
    drafts = {row['id']:row for row in eligible_drafts}
    if len(drafts) != len(eligible_drafts):
        raise ValueError('duplicate draft identifiers')
    output,audit = Path(output),Path(audit)
    checkpoint = output.parent/(output.stem+'-checkpoints')
    checkpoint.mkdir(parents=True,exist_ok=True)
    existing = {row['id']:row for row in read_cases(output)} if output.exists() else {}
    deadline = time.monotonic()+hours*3600
    merged,promoted,errors,missing = 0,0,0,0
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        for start in range(0,len(cases),workers):
            if time.monotonic() >= deadline:
                break
            jobs = []
            for case in cases[start:start+workers]:
                if case['id'] not in drafts:
                    missing += 1
                    continue
                stem = hashlib.sha256(case['id'].encode()).hexdigest()
                local_out,local_audit = checkpoint/(stem+'.jsonl'),checkpoint/(stem+'-audit.jsonl')
                future = executor.submit(review_annotations,[case],[drafts[case['id']]],adapter,local_out,local_audit,hours=max(0,(deadline-time.monotonic())/3600))
                jobs.append((case,future,local_out,local_audit))
            for case,future,local_out,local_audit in jobs:
                result = future.result()
                errors += result['request_errors']
                if not local_out.exists():
                    if local_audit.exists():
                        for audit_row in read_cases(local_audit):
                            append(audit,audit_row)
                    continue
                row = read_cases(local_out)[0]
                if case['id'] in existing:
                    if existing[case['id']]['annotation_review']['fingerprint'] != row['annotation_review']['fingerprint']:
                        raise ValueError('merged review fingerprint changed')
                    continue
                # These two aggregate append operations have one owning thread.
                for audit_row in read_cases(local_audit):
                    append(audit,audit_row)
                append(output,row)
                existing[case['id']] = row
                merged += 1
                promoted += row['label_status'] == 'agent-reviewed'
    return {'new_reviews':merged,'new_agent_reviewed':promoted,'request_errors':errors,'missing_drafts_unreviewed':missing,'not_human_certified':True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('cases');parser.add_argument('drafts');parser.add_argument('config')
    parser.add_argument('state');parser.add_argument('--hours',type=float,default=3)
    parser.add_argument('--output-name',default='source-reviewed')
    parser.add_argument('--stage-usd',type=float,default=10)
    parser.add_argument('--workers',type=int,default=4)
    parser.add_argument('--env-file')
    args = parser.parse_args()
    state = Path(args.state)
    state.mkdir(parents=True,exist_ok=True)
    config = json.loads(Path(args.config).read_text())
    config = {**config,'annotation_review':True,'schema':review_schema(),'stage_usd':args.stage_usd}
    session_path = state/'session.json'
    if not session_path.exists():
        raise ValueError('persisted session.json required; initialize the shared experiment session first')
    clock = json.loads(session_path.read_text())
    from .orchestrate import StageAdapter
    adapter = StageAdapter(Adapter(config,Ledger(state/'budget.sqlite'),state/'cache',**({'env_path':args.env_file} if args.env_file else {})),args.stage_usd,clock['inference_deadline_epoch'])
    cases = [case for case in read_cases(args.cases) if case.get('split') in ('validation','test')]
    result = review_annotations_parallel(cases,read_cases(args.drafts),adapter,
                                state/(args.output_name+'.jsonl'),state/(args.output_name+'-audit.jsonl'),hours=args.hours,workers=args.workers)
    print(canonical(result));print(canonical(adapter.adapter.ledger.summary()))


if __name__ == '__main__':
    main()
