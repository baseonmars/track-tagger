import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from experiments.parser.hosted import credential


class ExplicitCredentialFileTests(unittest.TestCase):
    def test_explicit_file_wins_over_inherited_account(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'fixture.env'
            path.write_text('UNRELATED=fixture\nexport OPENAI_API_KEY="file-placeholder"\n')
            with patch.dict('os.environ',{'OPENAI_API_KEY':'ambient-placeholder'}):
                self.assertEqual(credential(path),'file-placeholder')

    def test_missing_file_does_not_fall_back_to_ambient_account(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict('os.environ',{'OPENAI_API_KEY':'ambient-placeholder'}):
                with self.assertRaises(FileNotFoundError):
                    credential(Path(directory)/'absent.env')
