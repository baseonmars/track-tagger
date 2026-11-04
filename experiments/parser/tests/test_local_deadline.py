import json
from pathlib import Path
import tempfile
import unittest
from experiments.parser.pilot_local import remaining_seconds

class DeadlineTest(unittest.TestCase):
    def test_global_deadline_caps_and_expired_rejects(self):
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'session.json';p.write_text(json.dumps({'inference_deadline_epoch':110}))
            self.assertEqual(remaining_seconds(p,1,now=100),10)
            with self.assertRaises(ValueError):remaining_seconds(p,1,now=110)
