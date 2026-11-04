import concurrent.futures
import json
from pathlib import Path
import tempfile
import unittest
from experiments.parser.budget import Ledger, BudgetExceeded
from experiments.parser.scoring import identifier, reconcile, score, validate, evidence_score
from experiments.parser.runner import freeze, run

class HarnessTests(unittest.TestCase):
    def test_atomic_shared_cap_and_uncertain(self):
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger',1)
            def reserve(_):
                try:return ledger.reserve('attempt',.3)
                except BudgetExceeded:return None
            with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
                keys=[k for k in pool.map(reserve,range(20)) if k]
            self.assertEqual(len(keys),3)
            ledger.settle(keys[0])
            self.assertEqual(ledger.summary()['charged_usd'],.9)
            ledger.settle(keys[1],.1)
            self.assertAlmostEqual(ledger.summary()['charged_usd'],.7)
            self.assertRaises(BudgetExceeded,ledger.reserve,'retry',.4)
    def test_stage_cap_persists_across_instances(self):
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'ledger'
            ledger=Ledger(path)
            key=ledger.reserve('stage',.3,stage_cap=.4)
            ledger.settle(key,.25)
            self.assertRaises(BudgetExceeded,Ledger(path).reserve,'stage',.2,1)
            ledger.reserve('different-stage',.2,stage_cap=.4)

    def test_overrun_halts_all_workers(self):
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger')
            key=ledger.reserve('bad bound',.01)
            self.assertRaises(BudgetExceeded,ledger.settle,key,.02)
            self.assertRaises(BudgetExceeded,Ledger(Path(d)/'ledger').reserve,'next',.01)
    def test_conservative_mapping_and_dates(self):
        self.assertIsNone(identifier('1995-10-20.01.flac'))
        value={'titles':{'1:1':'Song'},'mappings':{},'review':False,'unresolved':[]}
        got=reconcile(value,['01.flac','01-copy.flac','unknown.flac'])
        self.assertTrue(got['review'])
        self.assertTrue(all(not x['title'] for x in got['mappings'].values()))
        self.assertEqual(validate({**value,'date':'2025-02-30'})['date'],'2025-02-30')
        self.assertEqual(reconcile({**value,'date':'2025-02-30'},[])['date'],'')
        self.assertEqual(reconcile({**value,'date':'2025-02'},[])['date'],'2025-02')
        self.assertEqual(reconcile({**value,'date':'2025'},[])['date'],'2025')
    def test_wrong_mapping_ready_vs_review(self):
        oracle={'titles':{},'mappings':{'a':{'disc':1,'number':1,'title':'Correct'}},'review':True}
        value={'titles':{},'mappings':{'a':{'disc':1,'number':1,'title':'Wrong'}},'review':True}
        self.assertEqual(score(value,oracle,['a'])['wrong_mappings'],1)
        self.assertEqual(score(value,oracle,['a'])['incorrect_accepted_mappings'],0)
        value['review']=False
        self.assertEqual(score(value,oracle,['a'])['incorrect_accepted_mappings'],1)
    def test_freeze_immutable_and_grouped(self):
        cases=[{'id':str(i),'group':str(i)} for i in range(8)]
        with tempfile.TemporaryDirectory() as d:
            out=Path(d)/'frozen.json'
            result=freeze(cases,out,counts=(4,2,2))
            self.assertEqual(len(result['cases']),8)
            self.assertEqual(len(result['pilot_ids']),2)
            self.assertRaises(ValueError,freeze,cases,out)
    def test_unreviewed_cases_not_accuracy(self):
        case={'id':'a','notes':[],'files':[],'oracle':{'review':False}}
        class Fake:
            def parse(self,*args):return {'output':{'titles':{},'mappings':{},'review':False,'unresolved':[]},'usage':{},'latency_seconds':0,'cache_hit':False}
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'rows'
            self.assertEqual(run([case],Fake(),path,'test')['accuracy_eligible'],0)
            self.assertEqual(run([case],Fake(),path,'test')['completed'],1)
    def test_evidence_quote_and_field_coverage(self):
        output={'artist':'Band','titles':{'1:1':'Song'},'evidence':[{'field':'artist','note':'a','quote':'Band'}]}
        self.assertEqual(evidence_score(output,[{'name':'a','text':'Band'}])['unsupported_populated_fields'],1)

if __name__=='__main__': unittest.main()

class HostedTests(unittest.TestCase):
    def test_http_usage_cache_and_gold_blinding(self):
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter
        payload={'artist':'','date':'','venue':'','city':'','titles':[],'mappings':[],'review':True,'unresolved':[],'evidence':[]}
        response={'status':'completed','model':'resolved-model','service_tier':'default','usage':{'input_tokens':100,'output_tokens':40,'input_tokens_details':{'cached_tokens':0,'cache_write_tokens':0}},'output':[{'content':[{'type':'output_text','text':json.dumps(payload)}]}]}
        class Response:
            def __enter__(self):return self
            def __exit__(self,*a):pass
            def read(self):return json.dumps(response).encode()
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger')
            cfg={'model':'fake','input_per_million':2,'output_per_million':10,'prices_verified':True}
            adapter=Adapter(cfg,ledger,Path(d)/'cache')
            case={'notes':[{'name':'n','text':'SOURCE','oracle':'SECRET'}],'files':[],'oracle':{'artist':'GOLD'}}
            captured=[]
            def call(req,**kwargs):captured.append(req.data);return Response()
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=call):
                result=adapter.parse(case,'prompt','candidate')
                cached=adapter.parse(case,'prompt','candidate')
                self.assertEqual(len(captured),1)
                self.assertNotIn(b'GOLD',captured[0]);self.assertNotIn(b'SECRET',captured[0])
                self.assertTrue(cached['cache_hit']);self.assertEqual(result['actual_model'],'resolved-model')
                self.assertAlmostEqual(ledger.summary()['charged_usd'],.0006)
                response['status']='incomplete'
                with self.assertRaises(RuntimeError):adapter.parse(case,'different','candidate')
                self.assertAlmostEqual(ledger.summary()['charged_usd'],.0012)

class RunnerSafetyTests(unittest.TestCase):
    def test_jsonl_and_fingerprint_reject_changed_prompt(self):
        from experiments.parser.runner import read_cases
        class Fake:
            config={}
            def parse(self,*args):return {'output':{'titles':{},'mappings':{},'review':True,'unresolved':[]},'usage':{},'latency_seconds':0,'cache_hit':False}
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'input';path.write_text('{"id":"a"}\n{"id":"b"}\n')
            self.assertEqual(len(read_cases(path)),2)
            case={'id':'a','notes':[],'files':[]}
            out=Path(d)/'out'
            run([case],Fake(),out,'phase',prompt='one')
            self.assertRaises(ValueError,run,[case],Fake(),out,'phase',prompt='two')
    def test_eligible_errors_remain_in_denominator(self):
        class Broken:
            def parse(self,*args):raise RuntimeError('test')
        case={'id':'a','notes':[],'files':[],'label_status':'synthetic','oracle':{'titles':{},'mappings':{},'review':False}}
        with tempfile.TemporaryDirectory() as d:
            result=run([case],Broken(),Path(d)/'out','phase')
            self.assertEqual(result['accuracy_eligible'],1)
            self.assertEqual(result['raw']['exact'],0)
            self.assertEqual(result['errors'],1)
    def test_catalog_identifier_parent_disc(self):
        from experiments.parser.adapters import catalog_identifiers
        binary=Path('/tmp/track-parser-adapter-v2')
        if not binary.exists():self.skipTest('build tagged adapter to verify catalog identity')
        ids=catalog_identifiers(binary,['Disc 2/01.flac'])
        self.assertEqual(ids,[(2,1)])

class FreezeAndConcurrencyTests(unittest.TestCase):
    def test_concurrent_annotation_resumes_single_writer(self):
        from experiments.parser.orchestrate import annotate_concurrently
        from unittest.mock import patch
        import threading,time
        active=0;peak=0;lock=threading.Lock()
        class Fake:config={}
        def make(case,adapter,output):
            nonlocal active,peak
            self.assertIsNone(output)
            with lock:active+=1;peak=max(peak,active)
            time.sleep(.002)
            with lock:active-=1
            return {'id':case['id'],'label_status':'draft'}
        cases=[{'id':str(i)} for i in range(20)]
        with tempfile.TemporaryDirectory() as d,patch('experiments.parser.orchestrate.draft',side_effect=make):
            output=Path(d)/'drafts'
            result=annotate_concurrently(cases,Fake(),output)
            self.assertEqual(result['new_annotations'],20)
            self.assertLessEqual(peak,4);self.assertGreater(peak,1)
            self.assertEqual(annotate_concurrently(cases,Fake(),output)['new_annotations'],0)
            self.assertEqual(len(output.read_text().splitlines()),20)
            with patch('experiments.parser.hosted.SCHEMA',{'changed':'schema'}):
                with self.assertRaises(ValueError):annotate_concurrently(cases,Fake(),output)
            self.assertEqual(len(output.read_text().splitlines()),20)
    def test_frozen_input_tampering_rejected(self):
        from experiments.parser.runner import read_cases
        with tempfile.TemporaryDirectory() as d:
            output=Path(d)/'frozen'
            result=freeze([{'id':str(i),'group':str(i)} for i in range(3)],output,counts=(1,1,1))
            result['cases'][0]['id']='tampered'
            output.write_text(json.dumps(result))
            self.assertRaises(ValueError,read_cases,output)
    def test_wrapper_minimum_fields_and_nonflac(self):
        value={'artist':'Artist','date':'2020','venue':'Hall','titles':{'1:1':'Song'},'mappings':{},'review':False,'unresolved':[]}
        self.assertFalse(reconcile(value,['01.flac'])['review'])
        self.assertTrue(reconcile({**value,'venue':''},['01.flac'])['review'])
        self.assertTrue(reconcile(value,[])['review'])
        self.assertTrue(reconcile(value,['01.wav'])['review'])

class BaselineSemanticsTests(unittest.TestCase):
    def test_production_baseline_not_reconciled(self):
        value={'artist':'Artist','date':'2020','venue':'Hall','titles':{'2:1':'Song'},'mappings':{'Disc 2/01.flac':{'disc':2,'number':1,'title':'Song'}},'review':False,'unresolved':[]}
        class Catalog:
            config={'kind':'catalog-baseline'}
            def parse(self,*args):return {'output':value,'usage':{},'cache_hit':False,'latency_seconds':.01}
        case={'id':'case','notes':[],'files':['Disc 2/01.flac'],'oracle':value,'label_status':'synthetic'}
        with tempfile.TemporaryDirectory() as d:
            out=Path(d)/'out';result=run([case],Catalog(),out,'baseline')
            self.assertEqual(result['validated']['exact'],1)
            self.assertFalse(json.loads(out.read_text())['validated']['review'])

class AmbiguitySemanticsTests(unittest.TestCase):
    def test_unresolved_prose_not_exact_label(self):
        value={'artist':'A','date':'2020','venue':'V','titles':{},'mappings':{},'review':True,'unresolved':['date conflict']}
        expected={**value,'unresolved':['conflicting source dates']}
        self.assertTrue(score(value,expected,[])['exact'])
        self.assertTrue(score({**value,'unresolved':['1:2']},expected,[])['exact'])
        self.assertFalse(score({**value,'unresolved':['1:2']},expected,[])['unresolved_ids_match'])
    def test_unresolved_always_requires_review(self):
        value={'artist':'A','date':'2020','venue':'V','titles':{'1:1':'Song'},'mappings':{},'review':False,'unresolved':['date conflict']}
        self.assertTrue(reconcile(value,['01.flac'])['review'])

class LabelFreezeTests(unittest.TestCase):
    def test_partition_metadata_and_source_hash_are_immutable(self):
        from experiments.parser.freeze import labels
        with tempfile.TemporaryDirectory() as d:
            d=Path(d);source=d/'source';reviewed=d/'reviews'
            frozen=freeze([{'id':str(i),'group':str(i),'features':[],'notes':[],'files':[]} for i in range(3)],source,counts=(1,1,1))
            case=next(c for c in frozen['cases'] if c['split']=='test')
            reviewed.write_text(json.dumps({**case,'split':'train','oracle':{},'label_status':'agent-reviewed'})+'\n')
            self.assertRaises(ValueError,labels,source,reviewed,d/'labels')
            reviewed.write_text(json.dumps({**case,'group':'changed','oracle':{},'label_status':'agent-reviewed'})+'\n')
            self.assertRaises(ValueError,labels,source,reviewed,d/'labels')
            reviewed.write_text(json.dumps({**case,'id':'unknown','oracle':{},'label_status':'agent-reviewed'})+'\n')
            self.assertRaises(ValueError,labels,source,reviewed,d/'labels')
            reviewed.write_text(json.dumps({**case,'oracle':{},'label_status':'disputed'})+'\n')
            labels(source,reviewed,d/'labels',d/'public')
            self.assertNotIn('notes',json.loads((d/'public').read_text())['cases'][0])

class PublicExportTests(unittest.TestCase):
    def test_original_quotes_and_rationale_are_dequoted(self):
        from experiments.parser.exports import dequote
        original='Unique raw prose never publish'
        value={'artist':'Band','titles':{},'mappings':{},'review':True,'unresolved':[original,'1:2'],
               'evidence':[{'field':'artist','note':'notes.txt','quote':original}]}
        result=dequote(value,[{'name':'notes.txt','text':'Header\n'+original+'\n'}])
        self.assertNotIn(original,json.dumps(result))
        self.assertEqual(result['unresolved'],['1:2'])
        self.assertEqual(result['evidence_references'][0]['line_offsets'],[{'start_line':2,'end_line':2}])

class EchoRedactionTests(unittest.TestCase):
    def test_echo_in_title_and_nonallowlisted_fields_not_exported(self):
        from experiments.parser.exports import predictions
        echo='Original source note line\nPrivate original next line'
        with tempfile.TemporaryDirectory() as d:
            source=Path(d)/'source';output=Path(d)/'public'
            source.write_text(json.dumps({'id':'a','debug_provider_text':echo,'raw':{'artist':echo,'titles':{'1:1':echo},'mappings':{'a.flac':{'disc':1,'number':1,'title':echo}},'review':False}})+'\n')
            predictions(source,output)
            text=output.read_text()
            self.assertNotIn('Original source note line',text)
            self.assertNotIn('debug_provider_text',text)
            self.assertIn('public_export_redactions',text)

class CachedPriceTests(unittest.TestCase):
    def test_cached_input_discount_and_unknown_details_conservative(self):
        from decimal import Decimal
        from experiments.parser.hosted import usage_cost
        config={'model':'gpt-6.1-sol','input_per_million':2,'output_per_million':10}
        discounted,method=usage_cost({'input_tokens':1000,'output_tokens':100,'input_tokens_details':{'cached_tokens':800,'cache_write_tokens':0}},config)
        self.assertEqual(discounted,Decimal('.00148'))
        self.assertEqual(method,'usage-with-cache-read-and-write-accounting-v2')
        conservative,method=usage_cost({'input_tokens':1000,'output_tokens':100},config)
        self.assertEqual(conservative,Decimal('.0035'))
        self.assertEqual(method,'conservative-30m-all-input')

class CandidateFreezeGuardTests(unittest.TestCase):
    def test_real_validation_dispatch_refuses_unfrozen_labels(self):
        from experiments.parser.guards import candidate_guard
        case={'id':'a','split':'validation','notes':[],'files':[],'label_status':'agent-reviewed'}
        self.assertRaises(ValueError,candidate_guard,case,'candidate-v1')
        candidate_guard(case,'annotation-independent-v1')
        candidate_guard({**case,'_labels_frozen_sha256':'abc'},'candidate-v1')
        self.assertRaises(ValueError,candidate_guard,{**case,'_labels_frozen_sha256':'abc','label_status':'unreviewed'},'candidate-v1')

class ReferenceRepairTests(unittest.TestCase):
    def test_unique_exact_source_reference_only(self):
        from experiments.parser.repair_references import repair
        value={'artist':'Band','date':'','venue':'','city':'','titles':{},'mappings':{},'review':True,'unresolved':[],
               'evidence':[{'field':'artist','note':'invented.txt','quote':'Band'}]}
        row={'id':'a','notes':[{'name':'actual.txt','text':'Band'}],'files':[],'oracle':value,'label_status':'disputed','annotation_review':{'verdict':'corrected'}}
        draft={'oracle':value}
        result,audit=repair(row,draft)
        self.assertEqual(result['oracle']['evidence'][0]['note'],'actual.txt')
        self.assertEqual(row['oracle']['evidence'][0]['note'],'invented.txt')
        self.assertEqual(result['label_status'],'agent-reviewed')
        row['notes'].append({'name':'second.txt','text':'Band'})
        result,audit=repair(row,draft)
        self.assertEqual(result['label_status'],'disputed')
        self.assertEqual(result['oracle']['evidence'][0]['note'],'invented.txt')

class WhitespaceScorerTests(unittest.TestCase):
    def test_comparison_only_whitespace_preserves_case_and_punctuation(self):
        from experiments.parser.scorer_v3 import score as v3
        gold={'artist':'The Band','date':'2020','venue':'The Hall','city':'A, ZZ','titles':{'1:1':'Song > Next'},'mappings':{'a':{'disc':1,'number':1,'title':'Song > Next'}},'review':False}
        output={**gold,'artist':'  The\tBand ','titles':{'1:1':' Song  >\nNext '},'mappings':{'a':{'disc':1,'number':1,'title':'Song\t> Next'}}}
        self.assertTrue(v3(output,gold,['a'])['exact'])
        self.assertEqual(output['artist'],'  The\tBand ')
        self.assertFalse(v3({**output,'artist':'the Band'},gold,['a'])['exact'])
        self.assertFalse(v3({**output,'titles':{'1:1':'Song Next'}},gold,['a'])['exact'])

class DynamicEvidenceSchemaTests(unittest.TestCase):
    def test_candidate_enum_is_copy_and_uses_no_gold(self):
        from experiments.parser.hosted import request_schema,SCHEMA
        original=json.dumps(SCHEMA,sort_keys=True)
        case={'notes':[{'name':'B.txt','text':'x'},{'name':'A.txt','text':'y'}],'oracle':{'artist':'GOLD'}}
        schema=request_schema({'evidence_note_enum':True},case)
        self.assertEqual(schema['properties']['evidence']['items']['properties']['note']['enum'],['A.txt','B.txt'])
        self.assertEqual(json.dumps(SCHEMA,sort_keys=True),original)
        self.assertNotIn('GOLD',json.dumps(schema))
        self.assertNotIn('enum',request_schema({},case)['properties']['evidence']['items']['properties']['note'])

class WriteAccountingTests(unittest.TestCase):
    def test_disjoint_cache_ttls_not_double_counted(self):
        from decimal import Decimal
        from experiments.parser.hosted import usage_cost
        config={'model':'gpt-6.1-sol','input_per_million':2,'output_per_million':10}
        usage={'input_tokens':1000,'output_tokens':100,'input_tokens_details':{'cached_tokens':100,'cache_write_tokens':800,'cache_write_tokens_details':{'ttl_30m_tokens':500,'ttl_12h_tokens':300}}}
        self.assertEqual(usage_cost(usage,config)[0],Decimal('.00396'))
        usage['input_tokens_details']['cache_write_tokens_details']['ttl_12h_tokens']=301
        self.assertRaises(ValueError,usage_cost,usage,config)
    def test_http429_circuit_and_explicit_zero_charge(self):
        from unittest.mock import patch
        import urllib.error,io
        from experiments.parser.hosted import Adapter,HostedFailure
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger');adapter=Adapter({'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True},ledger,Path(d)/'cache')
            error=urllib.error.HTTPError('https://api.openai.com/v1/responses',429,'ignored',{'retry-after':'60'},io.BytesIO(b'{"error":{"code":"rate_limit_exceeded","type":"tokens","message":"DO NOT LOG SOURCE"}}'))
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=error):
                with self.assertRaises(HostedFailure) as failure:adapter.parse({'notes':[],'files':[]},'health','health')
            self.assertEqual(failure.exception.result['failure']['http_status'],429)
            self.assertNotIn('DO NOT LOG SOURCE',json.dumps(failure.exception.result))
            self.assertEqual(ledger.summary()['charged_usd'],0)
            self.assertRaises(BudgetExceeded,ledger.reserve,'next',.01)

class ExplicitCreditAdmissionTests(unittest.TestCase):
    def test_credit_balance_quota_is_known_admission_not_unknown_transport(self):
        from experiments.parser.hosted import nonbillable_admission
        self.assertTrue(nonbillable_admission({'http_status':429,'error_code':'credit_balance_exhausted','error_type':'insufficient_quota'}))
        self.assertFalse(nonbillable_admission({'http_status':500,'error_code':'credit_balance_exhausted','error_type':'insufficient_quota'}))

class UnknownTransportTests(unittest.TestCase):
    def test_timeout_retains_full_reservation_and_halts(self):
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter,HostedFailure
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger');adapter=Adapter({'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True},ledger,Path(d)/'cache')
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=TimeoutError()):
                with self.assertRaises(HostedFailure):adapter.parse({'notes':[],'files':[]},'health','health')
            summary=ledger.summary()
            self.assertEqual(summary['statuses'],{'uncertain':1})
            self.assertGreater(summary['charged_usd'],0)
            self.assertRaises(BudgetExceeded,ledger.reserve,'next',.01)

class RetryRecoveryTests(unittest.TestCase):
    def test_transport_retry_once_preserves_original_and_identical_request(self):
        import io,urllib.error
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter,HostedFailure
        output={'artist':'','date':'','venue':'','city':'','titles':[],'mappings':[],'review':True,'unresolved':[],'evidence':[]}
        response={'status':'completed','model':'gpt-6-luna','usage':{'input_tokens':10,'output_tokens':20,'input_tokens_details':{'cached_tokens':0,'cache_write_tokens':0}},'output':[{'content':[{'type':'output_text','text':json.dumps(output)}]}]}
        class Response:
            def __enter__(self):return self
            def __exit__(self,*a):pass
            def read(self):return json.dumps(response).encode()
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger');cfg={'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True};adapter=Adapter(cfg,ledger,Path(d)/'cache');case={'notes':[],'files':[]};requests=[]
            error=urllib.error.HTTPError('https://api.openai.com/v1/responses',429,'ignored',{},io.BytesIO(b'{"error":{"code":"credit_balance_exhausted","type":"insufficient_quota"}}'))
            def fail(req,**kwargs):requests.append(req.data);raise error
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=fail):
                self.assertRaises(HostedFailure,adapter.parse,case,'prompt','original')
            primary=next((Path(d)/'cache').glob('*.json'));original=primary.read_bytes()
            with ledger.connect() as db:db.execute('DELETE FROM halt')
            adapter.config={**cfg,'retry_transport_once':True,'cache_phase_override':'original'}
            def success(req,**kwargs):requests.append(req.data);return Response()
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=success) as call:
                result=adapter.parse(case,'prompt','recovered-output-edition')
                again=adapter.parse(case,'prompt','recovered-output-edition')
                self.assertEqual(call.call_count,1)
            self.assertEqual(result['request_attempt'],2);self.assertTrue(again['cache_hit'])
            self.assertEqual(primary.read_bytes(),original);self.assertEqual(requests[0],requests[1])
            self.assertEqual(ledger.summary()['statuses'],{'settled':2})
    def test_incomplete_is_never_transport_retried(self):
        from experiments.parser.hosted import Adapter,HostedFailure,canonical
        import hashlib
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger');adapter=Adapter({'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True,'retry_transport_once':True},ledger,Path(d)/'cache')
            # First create a genuine cached incomplete outcome through mocked HTTP.
            from unittest.mock import patch
            class Response:
                def __enter__(self):return self
                def __exit__(self,*a):pass
                def read(self):return json.dumps({'status':'incomplete','usage':{'input_tokens':1,'output_tokens':1}}).encode()
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',return_value=Response()) as call:
                self.assertRaises(HostedFailure,adapter.parse,{'notes':[],'files':[]},'prompt','phase')
                self.assertRaises(HostedFailure,adapter.parse,{'notes':[],'files':[]},'prompt','phase')
                self.assertEqual(call.call_count,1)
    def test_stage_reallocation_does_not_raise_global_cap(self):
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger',.2);ledger.allocate_stage('phase',.4,'user-authorized recovery allocation')
            ledger.reserve('phase',.2,stage_cap=.4)
            self.assertRaises(BudgetExceeded,ledger.reserve,'phase',.01,.4)
            self.assertEqual(ledger.summary()['cap_usd'],.2)
    def test_cached_billing_recomputed_without_cache_mutation_or_debit(self):
        from experiments.parser.hosted import Adapter
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger');adapter=Adapter({'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True},ledger,Path(d)/'cache')
            row={'cost_usd':.00015,'usage':{'input_tokens':1000,'output_tokens':100,'input_tokens_details':{'cached_tokens':0,'cache_write_tokens':1000}}}
            path=Path(d)/'cache'/'old.json';path.write_text(json.dumps(row));original=path.read_bytes();updated=adapter.cached_result(row,path)
            self.assertAlmostEqual(updated['cost_usd'],.000175);self.assertEqual(updated['original_reported_cost_usd'],.00015)
            self.assertEqual(path.read_bytes(),original);self.assertEqual(ledger.summary()['charged_usd'],0)

class ExplicitBudgetAmendmentTests(unittest.TestCase):
    def test_authorized80_survives_reopen_preserves_cost_and_can_lower(self):
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'ledger';ledger=Ledger(path);key=ledger.reserve('old',1);ledger.settle(key,.5)
            self.assertRaises(ValueError,Ledger,path,80)
            ledger.authorize_total_80('Dan explicitly authorized TOTAL80, notadditional80',1234)
            reopened=Ledger(path)
            self.assertEqual(reopened.summary()['cap_usd'],80)
            self.assertEqual(reopened.summary()['charged_usd'],.5)
            self.assertEqual(Ledger(path,60).summary()['cap_usd'],60)
            self.assertRaises(ValueError,Ledger,path,80)
    def test_shared80_concurrent_enforcement(self):
        with tempfile.TemporaryDirectory() as d:
            path=Path(d)/'ledger';ledger=Ledger(path);ledger.authorize_total_80('explicit user amendment80',1234)
            def reserve(_):
                try:return Ledger(path).reserve('calls',7)
                except BudgetExceeded:return None
            with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:keys=[k for k in pool.map(reserve,range(20)) if k]
            self.assertEqual(len(keys),11)
            self.assertEqual(ledger.summary()['charged_usd'],77)
            self.assertEqual(ledger.summary()['cap_usd'],80)

class DurableDispatchClaimTests(unittest.TestCase):
    def test_primary_crash_claim_refuses_redispatch_without_more_charge(self):
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter,HostedFailure
        with tempfile.TemporaryDirectory() as d:
            path=Path(d);ledger=Ledger(path/'ledger');cfg={'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True}
            adapter=Adapter(cfg,ledger,path/'cache');case={'notes':[],'files':[]}
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=KeyboardInterrupt()):
                self.assertRaises(KeyboardInterrupt,adapter.parse,case,'prompt','phase')
            before=ledger.summary()['charged_usd']
            reopened=Adapter(cfg,Ledger(path/'ledger'),path/'cache')
            with patch('urllib.request.urlopen') as http:
                with self.assertRaises(HostedFailure) as failure:reopened.parse(case,'prompt','phase')
                self.assertEqual(http.call_count,0)
            self.assertEqual(failure.exception.result['failure']['kind'],'dispatch_claim_without_outcome')
            self.assertEqual(ledger.summary()['charged_usd'],before)
            self.assertEqual(ledger.summary()['statuses'],{'reserved':1})
    def test_retry_crash_claim_refuses_third_dispatch(self):
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter,HostedFailure
        import io,urllib.error
        with tempfile.TemporaryDirectory() as d:
            path=Path(d);ledger=Ledger(path/'ledger');cfg={'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True,'retry_transport_once':True};adapter=Adapter(cfg,ledger,path/'cache');case={'notes':[],'files':[]}
            error=urllib.error.HTTPError('https://api.openai.com/v1/responses',429,'ignored',{},io.BytesIO(b'{"error":{"code":"rate_limit_exceeded","type":"tokens"}}'))
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=error):self.assertRaises(HostedFailure,adapter.parse,case,'prompt','phase')
            with ledger.connect() as db:db.execute('DELETE FROM halt')
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',side_effect=KeyboardInterrupt()):self.assertRaises(KeyboardInterrupt,adapter.parse,case,'prompt','phase')
            before=ledger.summary()['charged_usd'];reopened=Adapter(cfg,Ledger(path/'ledger'),path/'cache')
            with patch('urllib.request.urlopen') as http:
                self.assertRaises(HostedFailure,reopened.parse,case,'prompt','phase');self.assertEqual(http.call_count,0)
            self.assertEqual(ledger.summary()['charged_usd'],before)
            self.assertEqual(ledger.summary()['statuses'],{'reserved':1,'settled':1})

class DurableCacheAndTTLTests(unittest.TestCase):
    def test_missing12h_details_never_uses30m_multiplier(self):
        from decimal import Decimal
        from experiments.parser.hosted import usage_cost
        cost,method=usage_cost({'input_tokens':1000,'output_tokens':100},{'model':'gpt-6.1-sol','input_per_million':2,'output_per_million':10,'cache_ttl':'12h'})
        self.assertEqual(cost,Decimal('.006'));self.assertEqual(method,'conservative-12h-all-input')
    def test_cache_fsyncs_file_and_parent(self):
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter
        with tempfile.TemporaryDirectory() as d:
            adapter=Adapter({'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True},Ledger(Path(d)/'ledger'),Path(d)/'cache')
            with patch('experiments.parser.hosted.fsync_directory') as sync:
                adapter.write_cache(Path(d)/'cache'/'result.json',{'ok':True})
                sync.assert_called_once_with(Path(d)/'cache')

class UnverifiedTierTests(unittest.TestCase):
    def test_unverified_tier_retains_full_reservation_before_settlement(self):
        from unittest.mock import patch
        from experiments.parser.hosted import Adapter,HostedFailure
        class Response:
            def __enter__(self):return self
            def __exit__(self,*a):pass
            def read(self):return json.dumps({'status':'completed','service_tier':'priority','usage':{'input_tokens':1,'output_tokens':1}}).encode()
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger');adapter=Adapter({'model':'gpt-6-luna','input_per_million':.1,'output_per_million':.5,'prices_verified':True},ledger,Path(d)/'cache')
            with patch('experiments.parser.hosted.credential',return_value='hidden'),patch('urllib.request.urlopen',return_value=Response()):self.assertRaises(HostedFailure,adapter.parse,{'notes':[],'files':[]},'health','health')
            summary=ledger.summary();self.assertEqual(summary['statuses'],{'uncertain':1});self.assertGreater(summary['charged_usd'],.001)
            self.assertRaises(BudgetExceeded,ledger.reserve,'next',.01)

class PublicFailurePrivacyTests(unittest.TestCase):
    def test_private_raw_response_path_and_message_not_exported(self):
        from experiments.parser.exports import predictions
        with tempfile.TemporaryDirectory() as d:
            source=Path(d)/'source';out=Path(d)/'public';source.write_text(json.dumps({'id':'a','phase':'test','error':'LocalFailure','failure':{'kind':'invalid_json','raw_response_path':'/private/original/raw-response.json','message':'source echo'}})+'\n')
            predictions(source,out);row=json.loads(out.read_text())
            self.assertEqual(row['failure'],{'kind':'invalid_json','raw_response_external':True})
            self.assertNotIn('/private/original',out.read_text());self.assertNotIn('source echo',out.read_text())
