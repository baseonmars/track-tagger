import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from .replay import reconstruct
from .labels import source_hash

class ReplayTests(unittest.TestCase):
    def fixtures(self):
        data=b'Artist\r\n';case={'notes':[{'name':'note.txt','text':'Artist\n'}],'files':['t01.flac']}
        record={'package_id':'fixture','notes':[{'path':'note.txt','source_url':'https://archive.org/download/fixture/note.txt','sha256':hashlib.sha256(data).hexdigest(),'size':len(data)}],'audio_files':[{'path':'t01.flac'}]}
        part={'group':'g','split':'test','files':case['files'],'source_input_sha256':source_hash(case)}
        return data,record,part
    def test_cached_original_replays_without_network_or_audio(self):
        data,record,part=self.fixtures()
        with tempfile.TemporaryDirectory()as d:
            root=Path(d);note=root/'packages/fixture/notes/note.txt';note.parent.mkdir(parents=True);note.write_bytes(data)
            with patch('experiments.parser.study_v2.replay.fetch')as fetch:
                result=reconstruct(record,part,root,'a'*64);fetch.assert_not_called()
            self.assertEqual(result['notes'][0]['text'],'Artist\n')
    def test_note_name_cannot_disguise_audio_url(self):
        _,record,part=self.fixtures();record['notes'][0]['source_url']='https://archive.org/download/fixture/t01.flac'
        with tempfile.TemporaryDirectory()as d,patch('experiments.parser.study_v2.replay.fetch')as fetch:
            with self.assertRaisesRegex(ValueError,'note URL'):reconstruct(record,part,Path(d),'a'*64)
            fetch.assert_not_called()
    def test_changed_cached_bytes_fail_instead_of_substitution(self):
        _,record,part=self.fixtures()
        with tempfile.TemporaryDirectory()as d:
            root=Path(d);note=root/'packages/fixture/notes/note.txt';note.parent.mkdir(parents=True);note.write_bytes(b'changed')
            with patch('experiments.parser.study_v2.replay.fetch')as fetch:
                with self.assertRaisesRegex(ValueError,'changed'):reconstruct(record,part,root,'a'*64)
                fetch.assert_not_called()

if __name__=='__main__':unittest.main()
