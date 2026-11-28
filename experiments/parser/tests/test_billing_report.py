import json
from pathlib import Path
import tempfile
import unittest
from experiments.parser.budget import Ledger
from experiments.parser.billing_report import report

class BillingCategoryTests(unittest.TestCase):
    def test_known_usage_unknown_ceiling_missing_usage_and_active_reconcile(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);ledger=Ledger(root/'budget.sqlite',2);(root/'cache').mkdir()
            known=ledger.reserve('phase:gpt-6-luna',.1);ledger.settle(known,.00006)
            (root/'cache'/'outcome.json').write_text(json.dumps({'reservation_id':known,'usage':{'input_tokens':100,'output_tokens':100,'input_tokens_details':{'cached_tokens':0,'cache_write_tokens':0}}}))
            uncertain=ledger.reserve('unknown:gpt-6-luna',.2);ledger.settle(uncertain)
            absent=ledger.reserve('lost:gpt-6-luna',.1);ledger.settle(absent,.03)
            active=ledger.reserve('active:gpt-6-luna',.1)
            result=report(root);values=result['categories_usd']
            self.assertAlmostEqual(values['known_usage_gross_list_price'],.00006)
            self.assertEqual(values['retained_unknown_maxima'],.2)
            self.assertEqual(values['settled_without_retained_usage_conservative'],.03)
            self.assertEqual(values['active_reservations'],.1)
            self.assertAlmostEqual(sum(values.values()),result['budget_footprint_usd'])
            self.assertIsNone(result['net_invoice_or_free_credit_consumption_usd'])
            with self.assertRaises(ValueError):report(root,True)
            ledger.settle(active,0);self.assertTrue(report(root,True)['final_drained_accounting'])
    def test_safe_probe_outcomes_are_accounted_without_cache_or_invoice_claim(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);ledger=Ledger(root/'budget.sqlite',2)
            denied=ledger.reserve('probe:gpt-6-luna',.1);ledger.settle(denied,0)
            denial=root/'denial.json';denial.write_text(json.dumps({'reservation_id':denied,'http_status':429,'error_type':'insufficient_quota','error_code':'credit_balance_exhausted'}))
            completed=ledger.reserve('health:gpt-6-luna',.1);ledger.settle(completed,.00006)
            health=root/'health.json';health.write_text(json.dumps({'reservation_id':completed,'usage':{'input_tokens':100,'output_tokens':100,'input_tokens_details':{'cached_tokens':0,'cache_write_tokens':0}}}))
            result=report(root,True,[denial,health])
            self.assertEqual(result['category_call_counts']['known_nonbillable_admissions'],1)
            self.assertEqual(result['category_call_counts']['known_usage_gross_list_price'],1)
            self.assertAlmostEqual(result['categories_usd']['known_usage_gross_list_price'],.00006)
            self.assertEqual(len(result['supplemental_outcome_sha256']),2)
