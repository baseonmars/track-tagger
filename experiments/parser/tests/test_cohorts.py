import hashlib,json,tempfile,unittest
from pathlib import Path
from experiments.parser.cohorts import test_cohort as select
from experiments.parser.budget import Ledger

class CohortTests(unittest.TestCase):
    def test_astra_default_is_common40_and_expansion_requires_policy(self):
        cases=[{'id':str(i),'split':'test'} for i in range(200)];manifest={'astra_ids':[str(i) for i in range(40)]}
        with tempfile.TemporaryDirectory() as d:
            ledger=Ledger(Path(d)/'ledger')
            self.assertEqual(len(select(cases,manifest,'gpt-6-astra',None,ledger,'unused','prompt','phase')),40)
            self.assertRaises(ValueError,select,cases,manifest,'gpt-6-astra','full200',ledger,'unused','prompt','phase')
    def test_expansion_budget_and_exact_frozen_inputs(self):
        cases=[{'id':str(i),'split':'test'} for i in range(200)];manifest={'astra_ids':[str(i) for i in range(40)]}
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);ledger=Ledger(root/'ledger');ledger.authorize_total_80('explicit80',1234);label=root/'labels';label.write_text(json.dumps(cases));policy=root/'policy';policy.write_text(json.dumps({'model':'gpt-6-astra','minimum_remaining_reserve_usd':5,'cost_padding_multiplier':1.25,'decision_basis':'budget/time only; no quality threshold'}));forecast=root/'forecast'
            value={'approved':True,'required_core_completed':True,'policy_sha256':hashlib.sha256(policy.read_bytes()).hexdigest(),'frozen_labels_sha256':hashlib.sha256(label.read_bytes()).hexdigest(),'prompt_sha256':hashlib.sha256(b'prompt').hexdigest(),'extra_case_ids':[str(i) for i in range(40,200)],'measured_mean_cost_usd':.1,'padded_additional_ceiling_usd':20};forecast.write_text(json.dumps(value))
            self.assertEqual(len(select(cases,manifest,'gpt-6-astra','full200',ledger,label,'prompt','phase',policy,forecast)),200)
            key=ledger.reserve('other',60);ledger.settle(key,60)
            self.assertRaises(ValueError,select,cases,manifest,'gpt-6-astra','full200',ledger,label,'prompt','phase',policy,forecast)
