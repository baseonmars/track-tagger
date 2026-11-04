"""Whitelist public numerical study results; never copy raw prediction/source text."""
import argparse
import hashlib
import json
import math
import re
from pathlib import Path

from .study_v2_evaluation import FIELDS

GATES = ('adjudicated_test_support', 'complete_track_reference_coverage',
         'reviewed_nonblank_reference_coverage', 'role_precision_and_support',
         'background_false_positive_rate', 'metadata_improvement', 'assistance_parity',
         'source_backed_proposals', 'source_eligible_metadata_proposals',
         'no_new_false_ready', 'no_new_material_acceptance', 'complete_candidate_outputs')


def number(value):
    if value is None:
        return None
    if type(value) not in (int, float) or not math.isfinite(value):
        raise ValueError('public metric must be a finite number or null')
    return value


def numeric_fields(value, names):
    if not isinstance(value, dict):
        raise ValueError('metric object required')
    return {key: number(value[key]) for key in names}


def interval(value):
    if value is None:
        return None
    if not isinstance(value, list) or len(value) != 2:
        raise ValueError('two numeric interval endpoints required')
    return [number(v) for v in value]


def sha(value):
    if not isinstance(value, str) or re.fullmatch('[0-9a-f]{64}', value) is None:
        raise ValueError('SHA256 required')
    return value


def case_hash(value):
    if not isinstance(value, str) or not value:
        raise ValueError('case identifier required')
    return hashlib.sha256(value.encode()).hexdigest()


def export_summary(result):
    """Unknown fields are discarded; schema/type mismatches fail closed.

    Human-readable source interpretations belong in independently authored reports,
    not an automatic copy of provider explanations, raw values or exception text.
    """
    if result.get('integration_approved') is not False:
        raise ValueError('quantitative exporter cannot authorize integration')
    arms = {}
    for name in ('baseline', 'fixed', 'assisted'):
        arms[name] = numeric_fields(result['arms'][name], ('eligible', 'metadata_exact', 'ready',
            'false_ready', 'failures', 'full_recording_gold_cases', 'recording_exact'))
    cohort = numeric_fields(result['review_cohort'], ('count', 'fixed_metadata_exact',
                                                       'proposed_metadata_exact', 'paired_improvement'))
    cohort['paired_group_bootstrap95'] = interval(result['review_cohort']['paired_group_bootstrap95'])
    roles = {}
    for role, values in result['roles'].items():
        if role not in FIELDS:
            raise ValueError('unexpected public metadata role')
        roles[role] = numeric_fields(values, ('tp', 'fp', 'fn', 'precision', 'recall', 'supporting_recordings'))
        roles[role]['precision_wilson95'] = interval(values['precision_wilson95'])
    gates = {key: result['gates'][key] for key in GATES}
    if any(type(value) is not bool for value in gates.values()):
        raise ValueError('boolean gates required')
    if type(result.get('quantitative_gates_pass')) is not bool or result['quantitative_gates_pass'] != all(gates.values()):
        raise ValueError('inconsistent quantitative gate result')
    failures = []
    for key, category in [('parity_failure_ids', 'underlying_output_changed'),
                          ('invalid_proposal_ids', 'invalid_source_proposal'),
                          ('proposal_failure_ids', 'proposal_runtime_failure'),
                          ('new_false_ready_ids', 'new_false_ready'),
                          ('new_material_acceptance_ids', 'new_accepted_value_mismatch')]:
        for case_id in result[key]:
            failures.append({'case_sha256': case_hash(case_id), 'category': category})
    for proposal in result['source_ineligible_proposals']:
        if proposal['field'] not in FIELDS:
            raise ValueError('unexpected proposal field')
        flags = proposal['flags']
        if not isinstance(flags, list) or not set(flags) <= {'mixed_scope', 'uncertainty', 'conflict', 'unresolved_role_reference'}:
            raise ValueError('unexpected source eligibility flag')
        failures.append({'case_sha256': case_hash(proposal['id']), 'category': 'source_ineligible_proposal',
                         'field': proposal['field'], 'flags': flags})
    return {'schema_version': 1, 'protocol_sha256': sha(result['protocol_sha256']),
            'arms': arms, 'review_cohort': cohort, 'roles': roles,
            'background': numeric_fields(result['background'], ('lines', 'false_positive_lines',
                'false_positive_rate', 'recordings', 'affected_recordings')),
            'reference_coverage': numeric_fields(result['reference_coverage'], ('requested_recordings',
                'eligible_recording_gold', 'source_lines', 'nonblank_lines',
                'reviewed_nondisputed_nonblank_lines', 'reviewed_nonblank_fraction')),
            'line_coverage': numeric_fields(result['line_coverage'], ('requested_lines', 'abstentions',
                'disputed_lines', 'failed_or_missing_lines')),
            'gates': gates, 'quantitative_gates_pass': result['quantitative_gates_pass'],
            'integration_approved': False, 'failure_index': failures}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('private_evaluation')
    parser.add_argument('public_summary')
    args = parser.parse_args()
    summary = export_summary(json.loads(Path(args.private_evaluation).read_text()))
    encoded = json.dumps(summary, indent=2, allow_nan=False) + '\n'
    # Do not accidentally replace an earlier public result.
    with Path(args.public_summary).open('x', encoding='utf-8') as stream:
        stream.write(encoded)
    print(json.dumps({'public_summary_sha256': hashlib.sha256(encoded.encode()).hexdigest()}))


if __name__ == '__main__':
    main()
