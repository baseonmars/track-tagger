import json
from pathlib import Path
import tempfile
import unittest
from experiments.parser.cohort_report import summarize

class RequestedReferenceDenominators(unittest.TestCase):
    def test_omitted_prediction_keeps_full_expected_title_and_mapping_counts(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);labels=root/'labels';predictions=root/'predictions'
            oracle={'titles':{'1:1':'Song'},'mappings':{'01.flac':{'disc':1,'number':1,'title':'Song'},'02.flac':{'disc':0,'number':0,'title':''}}}
            labels.write_text('\n'.join(json.dumps({'id':key,'label_status':'agent-reviewed','oracle':oracle,'files':['01.flac','02.flac']})for key in ('a','b')))
            score={'exact':True,'correct_titles':1,'expected_titles':1,'correct_mappings':2,'expected_mappings':2}
            predictions.write_text(json.dumps({'id':'a','raw':{'review':False},'validated':{'review':False},'raw_score':score,'validated_score':score})+'\n')
            result=summarize(predictions,labels)
            self.assertEqual(result['unattempted'],1)
            for stage in ('raw','validated'):
                self.assertEqual(result[stage]['expected_titles'],2)
                self.assertEqual(result[stage]['expected_mappings'],4)
                self.assertEqual(result[stage]['correct_titles'],1)
                self.assertEqual(result[stage]['correct_mappings'],2)
