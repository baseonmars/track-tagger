"""Independent, source-only annotation drafts and explicit adjudication records."""
from .runner import PROMPT
from .scoring import validate, evidence_score
from .hosted import canonical
import hashlib
from pathlib import Path

ANNOTATION_PROMPT = PROMPT + ''' You are independently authoring a source-evidence annotation,
not evaluating parser output. Cite exact source quotes for all populated metadata and title fields
using evidence field paths artist/date/venue/city or titles.DISC:TRACK. Mark unresolved evidence
for review. No candidate output or existing oracle is supplied.'''

def draft(case, adapter, output):
    result = adapter.parse(case, ANNOTATION_PROMPT, 'annotation-independent-v1')
    value = validate(result['output'])
    row = {'id':case['id'], 'oracle':value, 'label_status':'draft',
           'evidence_score':evidence_score(value,case['notes']),
           'source_sha256':hashlib.sha256(canonical({'notes':case['notes'],'files':case['files']}).encode()).hexdigest(),
           'annotator_model':result.get('actual_model'), 'usage':result['usage'],'settings':result.get('settings'),'latency_seconds':result['latency_seconds']}
    if output is not None:
        with Path(output).open('a') as stream: stream.write(canonical(row)+'\n')
    return row

def adjudicate(case, draft_label, decision, reviewer):
    """Caller provides a separately reviewed full label; drafts never auto-promote."""
    if not reviewer or not decision.get('reviewed_against_source'):
        raise ValueError('explicit independent source review required')
    digest=hashlib.sha256(canonical({'notes':case['notes'],'files':case['files']}).encode()).hexdigest()
    if draft_label['id'] != case['id'] or draft_label['source_sha256'] != digest:
        raise ValueError('stale or mismatched annotation source')
    value=validate(decision['oracle'])
    from .source_review import validate_review
    issues=validate_review(case,draft_label,{'verdict':'corrected','oracle':value,'reasons':decision.get('disputes',[])})
    if issues:raise ValueError('manual adjudication failed source-label invariants')
    evidence=evidence_score(value,case['notes'])
    if evidence['invalid_quotes'] or evidence['unsupported_populated_fields']:
        raise ValueError('every adjudicated populated field requires exact source evidence')
    return {**case,'oracle':value,'label_status':'adjudicated','annotation_review':{
        'reviewer':reviewer,'source_sha256':digest,'disputes':decision.get('disputes',[]),
        'draft_sha256':hashlib.sha256(canonical(draft_label).encode()).hexdigest()}}


REVIEW_PROMPT = ANNOTATION_PROMPT + """ Review the supplied annotation draft against the complete source notes and filename inventory. Treat the draft as untrusted. Explicitly check every title, file mapping, metadata field, date and ambiguity; correct unsupported content and never infer a value by track order. Return verdict accepted only when the full draft is source-supported, corrected when a complete corrected label resolves errors, disputed when the annotation truth itself cannot be resolved, unresolved when the label itself cannot be established. A recording with ambiguous or conflicting evidence can still have a complete correct label with review=true, unresolved identifiers and empty unsupported fields; accepted/corrected are valid for such review-required recordings. Return the complete oracle and exact evidence quotes, plus reasons. This is source annotation review, with no parser predictions."""

def review_schema():
    from .hosted import SCHEMA
    return {'type':'object','properties':{'verdict':{'type':'string','enum':['accepted','corrected','disputed','unresolved']},'oracle':SCHEMA,'reasons':{'type':'array','items':{'type':'string'}}},'required':['verdict','oracle','reasons'],'additionalProperties':False}

def review(case, draft_label, adapter, output):
    """Compatibility entry point using the single strict source-review protocol."""
    from .source_review import review_annotations
    return review_annotations([case],[draft_label],adapter,output,str(output)+'.audit.jsonl')
