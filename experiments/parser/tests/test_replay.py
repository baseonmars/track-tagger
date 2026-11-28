import hashlib,json,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
from experiments.parser.replay import replay
from experiments.parser.hosted import canonical

class ReplayTests(unittest.TestCase):
    def test_listed_notes_only_and_hashes(self):
        data=b'Artist\n01 Song\n';notes=[{'name':'n.txt','text':data.decode(),'encoding':'utf-8','source_encoding':'utf-8-sig'}];files=['01.flac']
        record={'package_id':'source','notes':[{'path':'n.txt','source_url':'https://archive.org/download/source/n.txt','sha256':hashlib.sha256(data).hexdigest(),'size':len(data)}],'audio_files':[{'path':'01.flac'}]}
        with tempfile.TemporaryDirectory() as directory:
            d=Path(directory);manifest=d/'manifest';manifest.write_text(json.dumps(record)+'\n')
            split=d/'split';split.write_text(json.dumps({'source_manifest_sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),'cases':[{'id':'source','group':'group','split':'test','input_sha256':hashlib.sha256(canonical({'notes':notes,'files':files}).encode()).hexdigest()}]}))
            with patch('experiments.parser.replay.fetch',return_value=data) as fetch:
                result=replay(manifest,split,d/'outside',workers=1)
                self.assertEqual(result['reconstructed'],1);self.assertTrue(result['all_input_hashes_verified'])
                self.assertEqual(fetch.call_args.args[0],'https://archive.org/download/source/n.txt')
                self.assertEqual(fetch.call_count,1)
            with patch('experiments.parser.replay.fetch',side_effect=AssertionError('cache should avoid network')):
                self.assertEqual(replay(manifest,split,d/'outside',workers=1)['reconstructed'],1)
            (d/'outside/packages/source/notes/n.txt').write_bytes(b'changed')
            self.assertEqual(replay(manifest,split,d/'outside',workers=1)['reconstructed'],0)
