import tempfile
import unittest
import hashlib
import time
from pathlib import Path
from ..budget import Ledger, BudgetExceeded
from .labels import flatten, lines
from .run_labels import initialize, ALLOCATIONS

class FullLineTests(unittest.TestCase):
    def setUp(self):
        self.case={'id':'fixture','group':'g','split':'train','notes':[{'name':'notes.txt','text':'\ufeffArtist\r\n\r\nVenue, City\r\n'}],'files':['t01.flac']}
        self.segments=[{'note':'notes.txt','start_line':i,'end_line':i+1,'roles':role,'uncertainty':False,'conflict':False,'mixed_scope':False,'role_disputed':False,'spans':spans} for i,role,spans in [(0,['artist'],[]),(1,['blank'],[]),(2,['venue','city'],[{'line_index':2,'start':0,'end':5,'roles':['venue']},{'line_index':2,'start':7,'end':11,'roles':['city']}])]]
    def test_normalization_preserves_interior_blanks(self):
        self.assertEqual(lines(self.case['notes'][0]['text']),['Artist','','Venue, City'])
        rows=flatten(self.case,{'titles':{},'mappings':{},'segments':self.segments})
        self.assertEqual(len(rows),3);self.assertEqual(rows[-1]['roles'],['venue','city'])
    def test_missing_line_and_invalid_unicode_span_rejected(self):
        with self.assertRaisesRegex(ValueError,'coverage'):flatten(self.case,{'segments':self.segments[:-1]})
        self.segments[-1]['spans'][0]['end']=100
        with self.assertRaisesRegex(ValueError,'span'):flatten(self.case,{'segments':self.segments})
    def test_factual_uncertainty_preserves_known_role(self):
        self.segments[0]['uncertainty']=True
        rows=flatten(self.case,{'segments':self.segments})
        self.assertEqual(rows[0]['roles'],['artist']);self.assertFalse(rows[0]['role_disputed'])
    def test_overlapping_quote_occurrences_are_ambiguous(self):
        case={**self.case,'notes':[{'name':'notes.txt','text':'aaa'}]}
        seg={**self.segments[0],'roles':['artist','venue'],'spans':[{'line_index':0,'quote':'aa','roles':['artist','venue']}]}
        with self.assertRaisesRegex(ValueError,'span'):flatten(case,{'segments':[seg]})
    def test_new_ledger_stage_caps_shield_independent_review(self):
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'budget.sqlite',50)
            allocations=ALLOCATIONS
            self.assertEqual(sum(allocations.values()),45)
            for name,cap in allocations.items():ledger.allocate_stage(name,cap,'frozen approved incremental50 allocation')
            key=ledger.reserve('annotation-v2-training:gpt-6.1-sol',10,10);ledger.settle(key,10)
            with self.assertRaises(BudgetExceeded):ledger.reserve('annotation-v2-training:gpt-6.1-sol',.000001,10)
            key=ledger.reserve('annotation-v2-independent-review:gpt-6.1-sol',13.5,13.5);ledger.settle(key,13.5)
            key=ledger.reserve('annotation-v2-independent-author:gpt-6.1-sol',11.5,11.5);ledger.settle(key,11.5)
            self.assertEqual(ledger.summary()['charged_usd'],35)
    def test_revised_phase_cannot_reset_pilot_or_change_allocation(self):
        with tempfile.TemporaryDirectory() as d:
            ledger,_=initialize(Path(d))
            key=ledger.reserve('annotation-v2-pilot-luna:gpt-6-luna',.5,.5);ledger.settle(key,.5)
            with self.assertRaises(BudgetExceeded):ledger.reserve('annotation-v2-pilot-luna-r2:gpt-6-luna',.000001,.5)
            with self.assertRaises(ValueError):ledger.reserve('annotation-v2-pilot-sol-r2:gpt-6.1-sol',.1,5)
            with self.assertRaises(ValueError):ledger.reserve('annotation-v2-unallocated:gpt-6-astra',.1,5)
            self.assertEqual(ledger.summary()['cap_usd'],50)
    def test_prior_amended_ledger_is_rejected_without_mutation(self):
        with tempfile.TemporaryDirectory()as d:
            path=Path(d)/'budget.sqlite';old=Ledger(path,50);old.authorize_total_80('test-only prior authorization',time.time())
            before=hashlib.sha256(path.read_bytes()).hexdigest()
            with self.assertRaisesRegex(ValueError,'prior or amended'):initialize(Path(d))
            self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(),before)
    def test_other_git_checkout_is_not_an_external_state_directory(self):
        with tempfile.TemporaryDirectory()as d:
            checkout=Path(d)/'other-project';(checkout/'.git').mkdir(parents=True)
            with self.assertRaisesRegex(ValueError,'outside Git'):initialize(checkout/'state')
            self.assertFalse((checkout/'state/budget.sqlite').exists())

if __name__=='__main__':unittest.main()
