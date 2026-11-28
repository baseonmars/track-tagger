"""OpenAI Responses adapter. Credentials are loaded only at request time."""
import hashlib
import copy
import json
import os
import math
import tempfile
from pathlib import Path
import time
import urllib.request
import urllib.error
import re
from decimal import Decimal
from .budget import BudgetExceeded

DEFAULT_ENV = Path('/Users/dan/code/track-tagger/.env')

def credential(path=DEFAULT_ENV):
    # Never execute dotenv text or put credentials in subprocess arguments.
    for line in Path(path).read_text().splitlines():
        key, sep, val = line.strip().removeprefix('export ').partition('=')
        if sep and key.strip() == 'OPENAI_API_KEY':
            return val.strip().strip('\"\'')
    raise ValueError('OPENAI_API_KEY unavailable')

def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'))

SCHEMA = {'type':'object','properties': {
    **{k:{'type':'string'} for k in ('artist','date','venue','city')},
    'titles':{'type':'array','items':{'type':'object','properties':{'id':{'type':'string'},'title':{'type':'string'}},'required':['id','title'],'additionalProperties':False}},
    'mappings':{'type':'array','items':{'type':'object','properties':{'path':{'type':'string'},'disc':{'type':'integer'},'number':{'type':'integer'},'title':{'type':'string'}},'required':['path','disc','number','title'],'additionalProperties':False}},
    'evidence':{'type':'array','items':{'type':'object','properties':{'field':{'type':'string'},'note':{'type':'string'},'quote':{'type':'string'}},'required':['field','note','quote'],'additionalProperties':False}},
    'review':{'type':'boolean'},'unresolved':{'type':'array','items':{'type':'string'}}},
    'required':['artist','date','venue','city','titles','mappings','review','unresolved','evidence'],'additionalProperties':False}

def request_schema(config,case):
    schema=copy.deepcopy(config.get('schema',SCHEMA))
    if config.get('line_annotation_note_enum'):
        names=sorted({n['name'] for n in case['notes']})
        if not names:raise ValueError('line annotation requires source documents')
        schema['properties']['segments']['items']['properties']['note']['enum']=names
    if config.get('source_gold_identifiers'):
        schema['properties']['titles']['items']['properties']['id']['pattern']=r'^[1-9]\d*:[1-9]\d*$'
        schema['properties']['mappings']['items']['properties']['path']['enum']=sorted(set(case['files']))
    if config.get('evidence_note_enum'):
        names=sorted({n['name'] for n in case['notes']})
        if not names:raise ValueError('evidence note enum requires source documents')
        schema['properties']['evidence']['items']['properties']['note']['enum']=names
    return schema

def fsync_directory(directory):
    # Directory handles are unavailable through this API on Windows; files still
    # fsync before atomic replacement. POSIX directory entries are synced too.
    if os.name=='nt':return False
    descriptor=os.open(directory,os.O_RDONLY|getattr(os,'O_DIRECTORY',0))
    try:os.fsync(descriptor)
    finally:os.close(descriptor)
    return True

CACHED_INPUT_RATES={'gpt-6-luna':Decimal('.01'),'gpt-6.1-sol':Decimal('.10'),'gpt-6-astra':Decimal('1')}

ACCOUNTING_VERSION=2

def usage_cost(usage,config):
    inp=Decimal(str(config['input_per_million']));out=Decimal(str(config['output_per_million']))
    total=usage.get('input_tokens');output=usage.get('output_tokens')
    if type(total)is not int or total<0 or type(output)is not int or output<0:raise ValueError('invalid usage totals')
    details=usage.get('input_tokens_details')
    if not isinstance(details,dict) or 'cached_tokens' not in details or 'cache_write_tokens' not in details:
        ttl=config.get('cache_ttl','30m')
        if ttl not in ('30m','12h'):raise ValueError('unknown cache TTL')
        factor=Decimal('1.25') if ttl=='30m' else Decimal('2.5')
        return (Decimal(total)*inp*factor+output*out)/1_000_000,'conservative-'+ttl+'-all-input'
    cached=details['cached_tokens'];writes=details['cache_write_tokens']
    if type(cached)is not int or type(writes)is not int or min(cached,writes)<0 or cached+writes>total:raise ValueError('inconsistent cache token usage')
    ttl=details.get('cache_write_tokens_details')
    if ttl is None:
        ttl_setting=config.get('cache_ttl','30m')
        if ttl_setting not in ('30m','12h'):raise ValueError('unknown cache write TTL')
        write30,write12=(writes,0) if ttl_setting=='30m' else (0,writes)
    else:
        if not isinstance(ttl,dict):raise ValueError('invalid cache TTL usage')
        write30,write12=ttl.get('ttl_30m_tokens',0),ttl.get('ttl_12h_tokens',0)
        if type(write30)is not int or type(write12)is not int or min(write30,write12)<0 or write30+write12!=writes:raise ValueError('inconsistent cache write TTL usage')
    read_rate=CACHED_INPUT_RATES.get(config['model'])
    if cached and read_rate is None:raise ValueError('unverified cached input rate')
    ordinary=total-cached-writes
    cost=(ordinary*inp+cached*(read_rate or Decimal(0))+write30*inp*Decimal('1.25')+write12*inp*Decimal('2.5')+output*out)/1_000_000
    return cost,'usage-with-cache-read-and-write-accounting-v2'


def safe_http_error(exc):
    result={'http_status':exc.code,'error_code':None,'error_type':None,'rate_limit_headers':{}}
    try:
        value=json.loads(exc.read(16384)).get('error',{})
        for source,target in (('code','error_code'),('type','error_type')):
            item=value.get(source)
            if isinstance(item,str) and re.fullmatch(r'[A-Za-z0-9_.-]{1,120}',item):result[target]=item
    except Exception:pass
    for key in ('retry-after','x-ratelimit-limit-requests','x-ratelimit-remaining-requests','x-ratelimit-reset-requests','x-ratelimit-limit-tokens','x-ratelimit-remaining-tokens','x-ratelimit-reset-tokens'):
        value=exc.headers.get(key) if exc.headers else None
        if value and len(value)<=160 and re.fullmatch(r'[0-9A-Za-z_. :/+,-]+',value):result['rate_limit_headers'][key]=value
    return result

def nonbillable_admission(error):
    return error.get('http_status')==429 and (error.get('error_code') in ('rate_limit_exceeded','insufficient_quota') or (error.get('error_code')=='credit_balance_exhausted' and error.get('error_type')=='insufficient_quota'))

class HostedFailure(RuntimeError):
    def __init__(self, result):
        self.result=result
        super().__init__(result['failure']['kind'])

class Adapter:
    def __init__(self, config, ledger, cache_dir, env_path=DEFAULT_ENV):
        self.config, self.ledger = config, ledger
        self.cache = Path(cache_dir)
        self.cache.mkdir(parents=True, exist_ok=True)
        self.env_path = env_path
        if any(not math.isfinite(float(config[k])) or float(config[k]) <= 0 for k in ('input_per_million','output_per_million')):
            raise ValueError('prices must be finite and positive')
        if config.get('prices_verified') is not True:
            raise ValueError('verified prices required')
        if config.get('service_tier', 'default') != 'default':
            raise ValueError('only verified standard service tier allowed')

    def parse(self, case, prompt, phase):
        from .guards import candidate_guard
        candidate_guard(case,phase)
        schema = request_schema(self.config,case)
        safe_case = {'notes':[{k:n[k] for k in ('name','text')} for n in case['notes']], 'files':case['files']}
        if self.config.get('annotation_review') or self.config.get('line_source_review'):
            safe_case['annotation_draft'] = case['annotation_draft']
        payload = {'model':self.config['model'], 'service_tier':'default', 'input':[
            {'role':'system','content':prompt}, {'role':'user','content':canonical(safe_case)}],
            'reasoning':{'effort':'low'},
            'max_output_tokens':self.config.get('max_output_tokens', min(16384,max(8192,2048+220*len(case['files'])))),
            'text':{'format':{'type':'json_schema','name':'concert_notes','strict':True,'schema':schema}}}
        body = canonical(payload).encode()
        cache_phase=self.config.get('cache_phase_override',phase)
        key = hashlib.sha256(cache_phase.encode() + b'\0' + body).hexdigest()
        cache_file = self.cache / (key + '.json')
        attempt=1
        if cache_file.exists():
            result=json.loads(cache_file.read_text())
            if 'failure' not in result:return self.cached_result(result,cache_file)
            retryable=result.get('failure',{}).get('kind')=='transport_or_decode'
            if not (self.config.get('retry_transport_once') and retryable):
                result['cache_hit']=True;raise HostedFailure(result)
            # Keep the original failure immutable. This separate key permits exactly
            # one retry with the identical model/prompt/schema/decoding payload.
            cache_file=self.cache/(key+'.transport-retry1.json')
            attempt=2
            if cache_file.exists():
                result=json.loads(cache_file.read_text())
                if 'failure' in result:
                    result['cache_hit']=True;raise HostedFailure(result)
                return self.cached_result(result,cache_file)
        upper_input = len(body) + 32768
        if upper_input + payload['max_output_tokens'] > 128000:
            raise ValueError('request exceeds conservative short-context ceiling')
        inp = Decimal(str(self.config['input_per_million']))
        out = Decimal(str(self.config['output_per_million']))
        ceiling = (Decimal(upper_input)*inp*Decimal('2.5') + payload['max_output_tokens']*out) / 1_000_000
        claim_file=cache_file.with_suffix('.claim.json')
        claim={'requested_model':self.config['model'],'phase':cache_phase,'request_attempt':attempt,'request_sha256':hashlib.sha256(body).hexdigest(),'created_epoch':time.time(),'pid':os.getpid()}
        try:
            descriptor=os.open(claim_file,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        except FileExistsError:
            if cache_file.exists():
                existing=json.loads(cache_file.read_text())
                if 'failure' not in existing:return self.cached_result(existing,cache_file)
                existing['cache_hit']=True;raise HostedFailure(existing)
            existing=json.loads(claim_file.read_text()) if claim_file.stat().st_size else {}
            raise HostedFailure({'failure':{'kind':'dispatch_claim_without_outcome'},'usage':{},'cache_hit':False,'latency_seconds':0,'request_attempt':attempt,'reservation_id':existing.get('reservation_id'),'phase':cache_phase,'requested_model':self.config['model']})
        with os.fdopen(descriptor,'w') as stream:
            stream.write(canonical(claim));stream.flush();os.fsync(stream.fileno())
        fsync_directory(self.cache)
        try:
            reservation = self.ledger.reserve(cache_phase + ':' + self.config['model'], ceiling, self.config.get('stage_usd'))
        except BudgetExceeded:
            # A rejected reservation proves no request was dispatched.
            claim_file.unlink()
            raise
        claim['reservation_id']=reservation
        self.write_cache(claim_file,claim)
        start = time.monotonic()
        raw={}
        usage={}
        step='transport'
        try:
            request = urllib.request.Request('https://api.openai.com/v1/responses', body,
                {'Authorization':'Bearer ' + credential(self.env_path), 'Content-Type':'application/json'})
            with urllib.request.urlopen(request, timeout=180) as response:
                raw = json.load(response)
            step='usage'
            usage = raw.get('usage', {})
            if type(usage.get('input_tokens')) is not int or type(usage.get('output_tokens')) is not int:
                raise ValueError('missing reliable billed usage')
            if raw.get('service_tier', 'default') != 'default':
                with self.ledger.connect() as db:
                    db.execute('INSERT INTO halt VALUES (?)', ('unverified returned service tier',))
                raise ValueError('unverified returned service tier')
            actual,cost_method=usage_cost(usage,self.config)
            self.ledger.settle(reservation, actual)
            step='output'
            if raw.get('status') != 'completed':
                raise ValueError('model did not complete structured response')
            texts = [c['text'] for item in raw.get('output',[]) for c in item.get('content',[]) if c.get('type') == 'output_text']
            output = json.loads(''.join(texts))
            target = output['oracle'] if self.config.get('annotation_review') else output
            titles, mappings = target['titles'], target['mappings']
            if len({x['id'] for x in titles}) != len(titles) or len({x['path'] for x in mappings}) != len(mappings):
                raise ValueError('duplicate model identifiers')
            target['titles'] = {x['id']:x['title'] for x in titles}
            target['mappings'] = {x['path']:{k:x[k] for k in ('disc','number','title')} for x in mappings}
        except BudgetExceeded:
            raise
        except Exception as exc:
            # One global circuit stops other workers before a provider failure can flood cases.
            if step in ('transport','usage'):
                with self.ledger.connect() as db:db.execute('INSERT INTO halt VALUES (?)',('provider-or-accounting circuit hold',))
            http=safe_http_error(exc) if isinstance(exc,urllib.error.HTTPError) else {}
            known_nonbillable=nonbillable_admission(http)
            if known_nonbillable:
                actual=Decimal(0);cost_method='explicit-nonbillable-429-admission'
                try:self.ledger.settle(reservation,0)
                except ValueError:pass
            else:
                try:self.ledger.settle(reservation)
                except ValueError:pass
            kind='transport_or_decode'
            if step=='usage':kind='usage_accounting'
            elif raw.get('status') and raw.get('status')!='completed':kind='incomplete'
            elif raw.get('status')=='completed':kind='output_schema'
            refused=any(c.get('type')=='refusal' for item in raw.get('output',[]) for c in item.get('content',[]))
            failure={'failure':{'kind':kind,**http,'status':raw.get('status'),'refusal':refused,
                'incomplete_reason':raw.get('incomplete_details',{}).get('reason') if isinstance(raw.get('incomplete_details'),dict) else None},
                'reservation_id':reservation,'requested_model':self.config['model'],'phase':cache_phase,'request_attempt':attempt,'accounting_version':ACCOUNTING_VERSION,'reserved_usd':float(ceiling),'usage':usage,'latency_seconds':time.monotonic()-start,'cache_hit':False,
                'actual_model':raw.get('model'),'cost_usd':float(actual) if 'actual' in locals() else None,'cost_method':cost_method if 'cost_method' in locals() else 'uncertain-reservation','settings':{'reasoning':'low','service_tier':'default','evidence_note_enum':bool(self.config.get('evidence_note_enum')),'max_output_tokens':payload['max_output_tokens']}}
            self.write_cache(cache_file,failure)
            raise HostedFailure(failure) from None
        result = {'output':output,'reservation_id':reservation,'requested_model':self.config['model'],'phase':cache_phase,'request_attempt':attempt,'accounting_version':ACCOUNTING_VERSION,'reserved_usd':float(ceiling),'usage':usage,'cache_hit':False,'latency_seconds':time.monotonic()-start,'actual_model':raw.get('model'),'cost_usd':float(actual) if 'actual' in locals() else None,'cost_method':cost_method if 'cost_method' in locals() else 'uncertain-reservation','settings':{'reasoning':'low','service_tier':'default','evidence_note_enum':bool(self.config.get('evidence_note_enum')),'max_output_tokens':payload['max_output_tokens']}}
        self.write_cache(cache_file,result)
        return result

    def cached_result(self,result,path):
        result={**result,'cache_hit':True,'cache_record_sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
        if result.get('usage'):
            cost,method=usage_cost(result['usage'],self.config)
            result.update(original_reported_cost_usd=result.get('cost_usd'),cost_usd=float(cost),cost_method=method,
                          original_accounting_version=result.get('accounting_version'),accounting_version=ACCOUNTING_VERSION,
                          cache_cost_recomputed_without_settlement=True)
        return result

    def write_cache(self,cache_file,result):
        with tempfile.NamedTemporaryFile(mode='w',dir=self.cache,prefix=cache_file.stem,suffix='.tmp',delete=False) as stream:
            stream.write(canonical(result));stream.flush();os.fsync(stream.fileno())
            temp = stream.name
        os.replace(temp, cache_file)
        fsync_directory(self.cache)
