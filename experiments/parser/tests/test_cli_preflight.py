import tempfile
import unittest
from pathlib import Path
from experiments.parser.runner import preflight_identifiers

class IdentifierPreflight(unittest.TestCase):
    def test_declared_helper_cannot_silently_use_fallback(self):
        with self.assertRaises(ValueError):
            preflight_identifiers({'identifier_adapter_source':'experiments/parser/adapter'})
        with tempfile.TemporaryDirectory() as directory:
            helper=Path(directory)/'helper';helper.write_text('fixture')
            with self.assertRaises(ValueError):preflight_identifiers({'identifier_executable':str(helper)})
            helper.chmod(0o700)
            preflight_identifiers({'identifier_adapter_source':'experiments/parser/adapter','identifier_executable':str(helper)})
