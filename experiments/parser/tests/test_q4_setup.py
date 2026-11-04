import signal
import subprocess
import unittest
from unittest.mock import Mock,patch
from experiments.parser.q4_setup import run_bounded

class Q4SetupTest(unittest.TestCase):
    def test_timeout_kills_owned_process_group(self):
        process=Mock(pid=123);process.wait.side_effect=[subprocess.TimeoutExpired(['tool'],1),0]
        with patch('experiments.parser.q4_setup.subprocess.Popen',return_value=process) as popen, patch('experiments.parser.q4_setup.os.killpg') as kill:
            with self.assertRaises(subprocess.TimeoutExpired):run_bounded(['tool'],1)
        popen.assert_called_once_with(['tool'],start_new_session=True)
        kill.assert_called_once_with(123,signal.SIGKILL)
        self.assertEqual(process.wait.call_count,2)
