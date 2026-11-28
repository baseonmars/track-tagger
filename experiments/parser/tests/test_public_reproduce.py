import hashlib,json,tempfile,unittest
from pathlib import Path
from experiments.parser.hosted import canonical
from experiments.parser.public_reproduce import join,check
from experiments.parser.scorer_v3 import score

class PublicReproductionTests(unittest.TestCase):
    def test_join_hash_and_semantic_check(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);case={'id':'a','group':'g','split':'test','notes':[{'name':'n.txt','text':'Band'}],'files':[]};value={'artist':'Band','date':'','venue':'','city':'','titles':{},'mappings':{},'review':True,'unresolved':[]}
            replay=root/'replay';replay.write_text(json.dumps(case)+'\n');gold=root/'gold';gold.write_text(json.dumps({'id':'a','group':'g','split':'test','label_status':'agent-reviewed','oracle':value,'source_input_sha256':hashlib.sha256(canonical({'notes':case['notes'],'files':[]}).encode()).hexdigest(),'selected_common40':True})+'\n')
            joined=root/'joined';self.assertEqual(join(replay,gold,joined)['joined'],1)
            scored=score(value,value,[]);pred=root/'pred';pred.write_text(json.dumps({'id':'a','phase':'test','accuracy_eligible':True,'raw':value,'validated':value,'raw_score':scored,'validated_score':scored})+'\n')
            result=check(pred,joined);self.assertEqual(result['semantic_metric_mismatches'],[]);self.assertEqual(result['unredacted_eligible_rows_semantically_checked'],1)
            case['notes'][0]['text']='tampered';replay.write_text(json.dumps(case)+'\n');self.assertRaises(ValueError,join,replay,gold,root/'newjoined')
    def test_duplicate_prediction_rows_rejected_for_aggregate_and_check(self):
        from experiments.parser.public_reproduce import aggregate
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);pred=root/'pred';row={'id':'a','phase':'test'};pred.write_text(json.dumps(row)+'\n'+json.dumps(row)+'\n');joined=root/'joined';joined.write_text(json.dumps({'id':'a','files':[],'notes':[]})+'\n')
            self.assertRaises(ValueError,aggregate,pred)
            self.assertRaises(ValueError,check,pred,joined)
