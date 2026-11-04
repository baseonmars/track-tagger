"""Complete source-line annotation schema and deterministic coverage validation."""
import hashlib
import re
from ..hosted import canonical, SCHEMA
import copy

ROLES=('artist','date','venue','city','title','heading','event','continuation','technical_provenance','other','blank')
FLAGS=('uncertainty','conflict','mixed_scope')

def normalize(text):
    return text.removeprefix('\ufeff').replace('\r\n','\n').replace('\r','\n')

def lines(text):
    result=normalize(text).split('\n')
    return result[:-1] if result and result[-1]=='' else result

def normalized_case(case):
    return {**case,'notes':[{**n,'text':normalize(n['text'])} for n in case['notes']]}

def source_hash(case):
    return hashlib.sha256(canonical({'notes':[{'name':n['name'],'text':normalize(n['text'])} for n in case['notes']],'files':case['files']}).encode()).hexdigest()

def schema(line_only=False):
    role={'type':'string','enum':list(ROLES)}
    span={'type':'object','properties':{'line_index':{'type':'integer'},'quote':{'type':'string'},'roles':{'type':'array','items':role}},'required':['line_index','quote','roles'],'additionalProperties':False}
    segment={'type':'object','properties':{'note':{'type':'string'},'start_line':{'type':'integer'},'end_line':{'type':'integer'},'roles':{'type':'array','items':role},**{f:{'type':'boolean'} for f in FLAGS},'role_disputed':{'type':'boolean'},'spans':{'type':'array','items':span}},'required':['note','start_line','end_line','roles',*FLAGS,'role_disputed','spans'],'additionalProperties':False}
    result = {'type':'object','properties':{'titles':{'type':'array','items':{'type':'object','properties':{'id':{'type':'string'},'title':{'type':'string'}},'required':['id','title'],'additionalProperties':False},'maxItems':0},'mappings':{'type':'array','items':{'type':'object','properties':{'path':{'type':'string'},'disc':{'type':'integer'},'number':{'type':'integer'},'title':{'type':'string'}},'required':['path','disc','number','title'],'additionalProperties':False},'maxItems':0},'segments':{'type':'array','items':segment}},'required':['titles','mappings','segments'],'additionalProperties':False}
    if not line_only:
        result['properties'].update(copy.deepcopy(SCHEMA['properties']))
        result['required']=list(SCHEMA['required'])+['segments']
    return result

PROMPT='''Annotate complete public concert notes for a full-line role-classification study. This is source labeling, not extraction or parser prediction. Alongside segments, independently author complete recording gold metadata, titles, mappings and source evidence; this is gold labeling, never a candidate prediction. For every supplied note cover EVERY normalized line exactly once with ordered nonoverlapping segments [start_line,end_line), zero-based. Each source line is prefixed with its zero-based index followed by a vertical bar (000000|); this display prefix is NOT source text and does not count toward span offsets. Input starts at line0; CRLF/CR normalize to LF, remove an initial BOM, omit exactly one terminal empty line. Blank interior lines count. Group adjacent lines only when roles and flags are identical. Roles may be multiple: artist (performer identity), date (concert date, including historical other-performance dates with mixed_scope=true, never transfer dates), venue, city, title (music track/song title), heading (section/set marker), event (festival/event identity), continuation (a continuation of a preceding title or field, usually also labeled its semantic role), technical_provenance (recording/transfer/checksum/lineage/credits/technical dates), other (positively known unrelated prose), blank (whitespace-only line). Other is NOT unknown. Preserve known role on uncertain facts: an uncertain venue remains venue with uncertainty=true. Use role_disputed=true only if the role itself cannot be determined; roles=[] is allowed only then. Conflict means contradictory source assertions. mixed_scope flags any line whose metadata is not unambiguously applicable to the current recording, including historical other-performance/LTP dates and locations, and studio/interview/album provenance. A known date/city/venue role remains positive on historical lines, but such spans are ineligible for current-recording metadata proposals. Flags are independent of roles. uncertainty/conflict apply uniformly to factual ambiguity in technical provenance as well as music metadata. Technical-only uncertainty and unambiguous historical cross-references do not alone require review of the current recording; current-recording ambiguity or actual mixed file provenance does. Technical durations/checksums/track statistics are not song titles merely because numbered. Unnumbered genuine song lists are titles, without inventing numeric file assignments. For multi-role lines include exact source spans {line_index,quote,roles}; quote must occur exactly once within the claimed normalized source line (without numeric display prefix). The validator derives Unicode codepoint [start,end) offsets from that unique exact quote; never count characters or guess offsets. Single-role line segments can use spans=[] meaning the whole line; never quote or rewrite source. Different roles may overlap when genuinely inseparable. No source files or lines may be omitted. Recording gold: artist/date/venue/city fields must be source-supported; date ISO YYYY or YYYY-MM or YYYY-MM-DD only, unknown empty. Retain city plus region when supplied, event plus physical venue when supported. Include every relative filename in mappings, with disc=0,number=0,title="" for every unknown/ambiguous JOIN even if the filename itself gives known numeric IDs. Preserve known file IDs only in source evidence, never partly-populated unknown mappings. Titles require explicit conservative disc:track IDs; never infer assignments by list position. Duplicate numbering, uncertain/conflicting titles or metadata, mixed recording provenance, missing mappings/metadata, or any non-FLAC file requires review=true. Unresolved reasons explain unknowns and review decisions. Exact source quotes (without display prefixes) cite each populated metadata field and title via evidence fields artist/date/venue/city/titles.DISC:TRACK with exact source note name. Preserve meaningful parenthetical cover credits and segue symbols exactly within song title text; do not remove them as decoration. Preserve unknowns rather than guess.'''

def flatten(case,value,status='agent-reviewed'):
    value=copy.deepcopy(value)
    by_note={n['name']:lines(n['text']) for n in case['notes']}
    if len(by_note)!=len(case['notes']):raise ValueError('duplicate source note names')
    seen=set();rows=[]
    for seg in value['segments']:
        name=seg['note'];start=seg['start_line'];end=seg['end_line'];roles=seg['roles']
        if name not in by_note or type(start)is not int or type(end)is not int or not 0<=start<end<=len(by_note[name]):raise ValueError('invalid line interval')
        if len(set(roles))!=len(roles) or set(roles)-set(ROLES) or (not roles and not seg['role_disputed']):raise ValueError('invalid roles')
        for f in (*FLAGS,'role_disputed'):
            if type(seg[f])is not bool:raise ValueError('invalid flag')
        for sp in seg['spans']:
            line=sp['line_index']
            if type(line)is not int or not start<=line<end:raise ValueError('invalid exact source span')
            if 'quote'in sp:
                quote=sp['quote'];source=by_note[name][line]
                if not isinstance(quote,str) or not quote:raise ValueError('invalid exact source span')
                a=source.find(quote)
                if a<0 or source.find(quote,a+1)>=0:raise ValueError('invalid exact source span')
                b=a+len(quote)
                sp.update(start=a,end=b,offset_provenance='unique-exact-quote-in-claimed-line')
            else:a=sp['start'];b=sp['end']
            if any(type(x)is not int for x in (a,b)) or not 0<=a<b<=len(by_note[name][line]) or not set(sp['roles'])<=set(roles) or not sp['roles'] or len(set(sp['roles']))!=len(sp['roles']):raise ValueError('invalid exact source span')
        for index in range(start,end):
            if (name,index)in seen:raise ValueError('duplicate line coverage')
            seen.add((name,index));line=by_note[name][index]
            if ('blank'in roles)!=(not line.strip()) or ('blank'in roles and len(roles)!=1):raise ValueError('blank role mismatch')
            if len(roles)>1 and not any(sp['line_index']==index for sp in seg['spans']):raise ValueError('multirole line missing exact spans')
            covered={role for sp in seg['spans'] if sp['line_index']==index for role in sp['roles']}
            if len(roles)>1 and covered!=set(roles) and not seg['role_disputed']:raise ValueError('multirole spans omit a role')
            rows.append({'id':case['id'],'group':case['group'],'split':case['split'],'note':name,'line_index':index,'line':line,'prev':by_note[name][index-1] if index else '', 'next':by_note[name][index+1] if index+1<len(by_note[name]) else '', 'roles':roles,'label_status':status,'source_input_sha256':source_hash(case),**{f:seg[f] for f in (*FLAGS,'role_disputed')},'spans':[sp for sp in seg['spans'] if sp['line_index']==index]})
    expected={(name,index) for name,ls in by_note.items() for index in range(len(ls))}
    if seen!=expected:raise ValueError('incomplete full-note coverage')
    return sorted(rows,key=lambda r:(r['note'],r['line_index']))
