import json
from pathlib import Path
import tempfile
import unittest
from experiments.parser.rescore import rescore

class RescoreShapeTest(unittest.TestCase):
    def test_reversed_inputs_rejected_before_output(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d); labels=root/'labels.jsonl'; predictions=root/'predictions.jsonl'; out=root/'out.jsonl'
            labels.write_text(json.dumps({'id':'x','notes':[],'files':[]})+'\n')
            predictions.write_text(json.dumps({'id':'x','phase':'pilot','raw':{},'validated':{}})+'\n')
            with self.assertRaises(ValueError): rescore(labels,predictions,out)
            self.assertFalse(out.exists())
    def test_duplicate_prediction_ids_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d); labels=root/'labels.jsonl'; predictions=root/'predictions.jsonl'; out=root/'out.jsonl'
            labels.write_text(json.dumps({'id':'x','notes':[],'files':[]})+'\n')
            row=json.dumps({'id':'x','phase':'pilot','error':'ValueError'})+'\n';predictions.write_text(row+row)
            with self.assertRaises(ValueError):rescore(predictions,labels,out)
            self.assertFalse(out.exists())
