import json
import tempfile
import unittest
from pathlib import Path
from experiments.parser.feature_report import summarize

class SourceFeatureStrataTests(unittest.TestCase):
    def test_errors_stay_in_source_strata_and_overlaps_are_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            labels=Path(directory)/'labels';preds=Path(directory)/'preds'
            labels.write_text('\n'.join(json.dumps(c) for c in [{'id':'a','features':['disc','notes'],'label_status':'agent-reviewed','oracle':{'review':False}},{'id':'b','features':['notes'],'label_status':'agent-reviewed','oracle':{'review':False}}]))
            preds.write_text('\n'.join(json.dumps(c) for c in [{'id':'a','error':'failure'},{'id':'b','raw':{'review':False},'validated':{'review':True},'raw_score':{'exact':True},'validated_score':{'exact':False}}]))
            result=summarize(preds,labels)['strata']
            self.assertEqual(result['disc']['eligible'],1);self.assertEqual(result['disc']['errors'],1)
            self.assertEqual(result['notes']['eligible'],2);self.assertEqual(result['notes']['raw']['exact'],1)
            self.assertEqual(result['notes']['raw']['ready'],1);self.assertEqual(result['notes']['validated']['ready'],0)
    def test_duplicate_predictions_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            labels=Path(directory)/'labels';preds=Path(directory)/'preds';labels.write_text('[]');preds.write_text('{"id":"a"}\n{"id":"a"}\n')
            with self.assertRaises(ValueError):summarize(preds,labels)
