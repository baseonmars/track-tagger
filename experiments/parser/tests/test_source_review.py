import copy
import json
from pathlib import Path
import tempfile
import unittest
from experiments.parser import source_review as review


def fixture():
    case = {'id':'synthetic-review','split':'validation','notes':[{'name':'notes.txt','text':'Artist: Fixture\nDate: 2020-01-02\n01. Song A\nVenue: Hall','encoding':'utf-8'}],'files':['t01.flac']}
    oracle = {'artist':'Fixture','date':'2020-01-02','venue':'Hall','city':'','titles':{'1:1':'Song A'},'mappings':{'t01.flac':{'disc':1,'number':1,'title':'Song A'}},'review':False,'unresolved':[],'evidence':[{'field':'artist','note':'notes.txt','quote':'Artist: Fixture'},{'field':'venue','note':'notes.txt','quote':'Venue: Hall'},{'field':'date','note':'notes.txt','quote':'Date: 2020-01-02'},{'field':'titles.1:1','note':'notes.txt','quote':'01. Song A'}]}
    draft = {'id':case['id'],'label_status':'draft','oracle':copy.deepcopy(oracle),'source_sha256':review.source_hash(case)}
    response = {'verdict':'accepted','oracle':oracle,'reasons':['Every field checked against complete source.']}
    return case,draft,response


class FakeAdapter:
    def __init__(self,response):
        self.response = response
        self.calls = []
        self.config = {'model':'gpt-6.1-sol','annotation_review':True,'prices_verified':True,'input_per_million':2,'output_per_million':10}
    def parse(self,case,prompt,phase):
        self.calls.append(case)
        return {'output':self.response,'usage':{'input_tokens':10,'output_tokens':20},'settings':{'reasoning':'low'},'actual_model':'sol-fixture','cache_hit':False}


class SourceReviewTests(unittest.TestCase):
    def test_complete_source_review_qualifies(self):
        case,draft,response = fixture()
        self.assertEqual(review.validate_review(case,draft,response),[])

    def test_correct_ambiguous_safe_oracle_remains_eligible(self):
        case,draft,response = fixture()
        oracle = {'artist':'','date':'','venue':'','city':'','titles':{},'mappings':{'t01.flac':{'disc':0,'number':0,'title':''}},'review':True,'unresolved':['ambiguous source identifiers'],'evidence':[]}
        draft['oracle'] = copy.deepcopy(oracle)
        response['oracle'] = oracle
        self.assertEqual(review.validate_review(case,draft,response),[])
        with tempfile.TemporaryDirectory() as temporary:
            output,audit = Path(temporary)/'r',Path(temporary)/'a'
            result = review.review_annotations([case],[draft],FakeAdapter(response),output,audit)
            self.assertEqual(result['new_agent_reviewed'],1)
            self.assertTrue(json.loads(output.read_text())['oracle']['review'])

    def test_noncanonical_oracle_title_ids_never_promote(self):
        for key in ('0:1','1:0','01:1','one:two'):
            case,draft,response = fixture()
            response['oracle']['titles'][key] = 'Unknown'
            response['oracle']['review'] = True
            self.assertIn('invalid-oracle-title-identifiers',review.validate_review(case,draft,response))

    def test_missing_and_foreign_file_inventory_block(self):
        case,draft,response = fixture()
        response['oracle']['mappings'] = {'other.flac':{'disc':1,'number':1,'title':'Song A'}}
        self.assertIn('incomplete-or-foreign-file-inventory',review.validate_review(case,draft,response))

    def test_unquoted_title_and_invalid_date_block(self):
        case,draft,response = fixture()
        response['oracle']['date'] = '2020-02-30'
        response['oracle']['evidence'] = []
        issues = review.validate_review(case,draft,response)
        self.assertIn('missing-source-citations',issues)
        self.assertIn('invalid-calendar-date',issues)

    def test_nonflac_unresolved_and_unmapped_require_review(self):
        case,draft,response = fixture()
        case['files'] = ['t01.mp3']
        response['oracle']['mappings'] = {'t01.mp3':{'disc':0,'number':0,'title':''}}
        response['oracle']['unresolved'] = ['missing title']
        issues = review.validate_review(case,draft,response)
        for expected in ('non-flac-package-requires-review','unresolved-evidence-requires-review','unmapped-file-requires-review'):
            self.assertIn(expected,issues)

    def test_accepted_cannot_change_draft(self):
        case,draft,response = fixture()
        response['oracle']['artist'] = ''
        self.assertIn('accepted-verdict-changes-draft',review.validate_review(case,draft,response))
        response['verdict'] = 'corrected'
        response['oracle']['review'] = True
        self.assertEqual(review.validate_review(case,draft,response),[])

    def test_review_is_audited_and_resumable(self):
        case,draft,response = fixture()
        adapter = FakeAdapter(response)
        with tempfile.TemporaryDirectory() as temporary:
            output,audit = Path(temporary)/'reviews.jsonl',Path(temporary)/'audit.jsonl'
            result = review.review_annotations([case],[draft],adapter,output,audit)
            self.assertEqual(result['new_agent_reviewed'],1)
            record = json.loads(output.read_text())
            self.assertEqual(record['label_status'],'agent-reviewed')
            self.assertTrue(record['annotation_review']['not_human_certified'])
            self.assertEqual(json.loads(audit.read_text())['reviewer_response'],response)
            self.assertEqual(adapter.calls[0]['annotation_draft'],draft['oracle'])
            self.assertNotIn('raw',adapter.calls[0])
            self.assertEqual(review.review_annotations([case],[draft],adapter,output,audit)['new_reviews'],0)
            self.assertEqual(len(adapter.calls),1)

    def test_disputed_and_unresolved_never_qualify(self):
        for verdict in ('disputed','unresolved'):
            case,draft,response = fixture()
            response['verdict'] = verdict
            with tempfile.TemporaryDirectory() as temporary:
                output,audit = Path(temporary)/'r',Path(temporary)/'a'
                result = review.review_annotations([case],[draft],FakeAdapter(response),output,audit)
                self.assertEqual(result['new_agent_reviewed'],0)
                self.assertNotEqual(json.loads(output.read_text())['label_status'],'agent-reviewed')

    def test_parallel_single_writer_checkpoint_resume(self):
        case,draft,response = fixture()
        cases,drafts = [],[]
        for i in range(5):
            c,d = copy.deepcopy(case),copy.deepcopy(draft)
            c['id'] = d['id'] = 'synthetic-review-'+str(i)
            cases.append(c);drafts.append(d)
        adapter = FakeAdapter(response)
        with tempfile.TemporaryDirectory() as temporary:
            output,audit = Path(temporary)/'r.jsonl',Path(temporary)/'a.jsonl'
            result = review.review_annotations_parallel(cases,drafts,adapter,output,audit,workers=4)
            self.assertEqual(result['new_agent_reviewed'],5)
            self.assertEqual(len(output.read_text().splitlines()),5)
            self.assertEqual(len(audit.read_text().splitlines()),5)
            self.assertEqual(len(adapter.calls),5)
            self.assertEqual(review.review_annotations_parallel(cases,drafts,adapter,output,audit)['new_reviews'],0)
            self.assertEqual(len(adapter.calls),5)

    def test_parallel_missing_or_failed_drafts_stay_unreviewed(self):
        case,draft,response = fixture()
        failed = {'id':case['id'],'label_status':'error'}
        with tempfile.TemporaryDirectory() as temporary:
            output,audit = Path(temporary)/'r',Path(temporary)/'a'
            adapter = FakeAdapter(response)
            result = review.review_annotations_parallel([case],[failed],adapter,output,audit)
            self.assertEqual(result['missing_drafts_unreviewed'],1)
            self.assertEqual(adapter.calls,[])
            self.assertFalse(output.exists())

    def test_missing_venue_and_empty_audio_require_review(self):
        case,draft,response = fixture()
        case['files'] = []
        response['oracle']['venue'] = ''
        response['oracle']['mappings'] = {}
        issues = review.validate_review(case,draft,response)
        self.assertIn('missing-venue-requires-review',issues)
        self.assertIn('empty-audio-inventory-requires-review',issues)

    def test_stale_draft_and_training_source_refused(self):
        case,draft,response = fixture()
        draft['source_sha256'] = 'stale'
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError,'stale source'):
                review.review_annotations([case],[draft],FakeAdapter(response),Path(temporary)/'r',Path(temporary)/'a')
            case['split'] = 'train'
            with self.assertRaisesRegex(ValueError,'validation/test'):
                review.review_annotations([case],[draft],FakeAdapter(response),Path(temporary)/'r',Path(temporary)/'a')

    def test_unverified_price_and_candidate_rows_refused(self):
        case,draft,response = fixture()
        adapter = FakeAdapter(response)
        adapter.config['prices_verified'] = False
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError,'verified Sol'):
                review.review_annotations([case],[draft],adapter,Path(temporary)/'r',Path(temporary)/'a')
            adapter.config['prices_verified'] = True
            draft['label_status'] = 'candidate'
            with self.assertRaisesRegex(ValueError,'annotation drafts only'):
                review.review_annotations([case],[draft],adapter,Path(temporary)/'r',Path(temporary)/'a')


if __name__ == '__main__':
    unittest.main()
