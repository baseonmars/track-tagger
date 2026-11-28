"""Include the source-only study guards in the normal offline test suite."""
from experiments.parser.study_v2.test_components import ComponentTests
from experiments.parser.study_v2.test_freeze import BindingTests
from experiments.parser.study_v2.test_labels import FullLineTests
from experiments.parser.study_v2.test_neighbors import NeighborTests

from experiments.parser.study_v2.test_gold import GoldTests
from experiments.parser.study_v2.test_replay import ReplayTests
from experiments.parser.study_v2.test_refs import ReferenceTests
