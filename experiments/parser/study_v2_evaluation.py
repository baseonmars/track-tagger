"""Source-frozen three-arm evaluation. No inference, annotation, or paid calls.

All inputs/outputs containing source lines or predictions belong outside Git.
A protocol binds reviewed recording and complete-line gold before predictions.
"""
import hashlib
import json
import math
import random

FIELDS = ('artist', 'date', 'venue', 'city')
BASELINE_COMMIT = '5fd190bc13e519edc05788d2bd8cf57c2f23bb28'
FIXED_COMMIT = 'fece1a07e861390c592edf1acac14cf18faabecb'
REVIEWED = {'agent-reviewed', 'adjudicated'}
ROLES = set(FIELDS) | {'title', 'heading', 'event', 'continuation', 'technical_provenance', 'other', 'blank'}
BACKGROUND = {'other', 'technical_provenance', 'blank'}
PARITY_FIELDS = ('metadata', 'ready', 'review_reasons', 'mappings', 'write_plan')


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                     separators=(',', ':'), allow_nan=False).encode()).hexdigest()


def indexed(rows):
    result = {}
    for row in rows:
        key = row['id']
        if key in result:
            raise ValueError('duplicate recording ID')
        result[key] = row
    return result


def line_key(row):
    return row['id'], row['note'], row['line_index']


def indexed_lines(rows):
    result = {}
    for row in rows:
        key = line_key(row)
        if key in result:
            raise ValueError('duplicate source line')
        result[key] = row
    return result


def valid_metadata(value):
    return isinstance(value, dict) and set(value) == set(FIELDS) and all(
        isinstance(value[f], str) for f in FIELDS)


def normalized(value):
    return ' '.join(value.split())


def metadata_equal(left, right):
    return valid_metadata(left) and all(normalized(left[f]) == normalized(right[f]) for f in FIELDS)


def freeze_protocol(sources, recording_gold, line_gold, enabled_roles=FIELDS, *, classifier_model_sha256, execution_manifest):
    """Call before predictions; source_line_counts must come from actual input.

    Disputed recording labels stay in the requested cohort but are ineligible.
    Unknown values are explicit empty strings, never omitted metadata members.
    """
    if not isinstance(classifier_model_sha256, str) or len(classifier_model_sha256) != 64:
        raise ValueError('classifier model hash required')
    try:
        bytes.fromhex(classifier_model_sha256)
    except ValueError:
        raise ValueError('invalid classifier model hash') from None
    expected_execution_keys = {'runner_sha256', 'build_manifest_sha256', 'baseline_binary_sha256', 'fixed_binary_sha256', 'classifier_binary_sha256', 'model_freeze_sha256'}
    if not isinstance(execution_manifest, dict) or set(execution_manifest) != expected_execution_keys:
        raise ValueError('frozen execution manifest required')
    for value in execution_manifest.values():
        if not isinstance(value, str) or len(value) != 64:
            raise ValueError('invalid executable hash')
        try:
            if len(bytes.fromhex(value)) != 32:
                raise ValueError('invalid executable hash')
        except ValueError:
            raise ValueError('invalid executable hash') from None
    source_by_id, gold = indexed(sources), indexed(recording_gold)
    lines = indexed_lines(line_gold)
    if set(source_by_id) != set(gold):
        raise ValueError('source/gold recording coverage mismatch')
    enabled_roles = list(enabled_roles)
    if not enabled_roles or len(set(enabled_roles)) != len(enabled_roles) or not set(enabled_roles) <= set(FIELDS):
        raise ValueError('invalid enabled metadata roles')
    expected_lines = set()
    eligible = []
    review_cohort = []
    for case_id, src in source_by_id.items():
        label = gold[case_id]
        if src.get('split', 'test') != 'test' or label.get('split', 'test') != 'test':
            raise ValueError('only sealed test sources allowed')
        if label.get('source_input_sha256') != src['source_input_sha256']:
            raise ValueError('stale recording gold')
        if not valid_metadata(label.get('metadata')) or type(label.get('review_required')) is not bool:
            raise ValueError('incomplete recording gold')
        if label.get('label_status') not in REVIEWED | {'disputed', 'unresolved'}:
            raise ValueError('recording gold has not been independently reviewed')
        if label['label_status'] in REVIEWED:
            eligible.append(case_id)
            if label['review_required']:
                review_cohort.append(case_id)
        if not src.get('group') or not isinstance(src.get('notes'), dict) or not src['notes']:
            raise ValueError('source grouping/line counts required')
        for note, count in src['notes'].items():
            if type(count) is not int or count < 0:
                raise ValueError('invalid source line count')
            expected_lines.update((case_id, note, i) for i in range(count))
    if set(lines) != expected_lines:
        raise ValueError('complete line gold coverage required')
    for key, row in lines.items():
        if row.get('source_input_sha256') != source_by_id[key[0]]['source_input_sha256']:
            raise ValueError('stale line gold')
        if row.get('label_status') not in REVIEWED | {'disputed', 'unresolved'}:
            raise ValueError('line gold has not been independently reviewed')
        if row.get('label_status') not in REVIEWED and (row.get('roles') != [] or row.get('role_disputed') is not True):
            raise ValueError('unknown line gold must have empty roles and explicit dispute')
        if (not isinstance(row.get('line'), str) or type(row.get('line_index')) is not int or not isinstance(row.get('roles'), list)
                or any(not isinstance(r, str) for r in row['roles']) or set(row['roles']) - ROLES
                or len(set(row['roles'])) != len(row['roles']) or type(row.get('role_disputed')) is not bool):
            raise ValueError('invalid line gold')
        if any(type(row.get(flag)) is not bool for flag in ('mixed_scope', 'uncertainty', 'conflict')):
            raise ValueError('explicit source applicability flags required')
        if not row['roles'] and not row['role_disputed']:
            raise ValueError('unlabeled line must remain explicitly disputed')
        if row['roles'] and (('blank' in row['roles']) != (not row['line'].strip()) or ('blank' in row['roles'] and len(row['roles']) != 1)):
            raise ValueError('blank line label mismatch')
    return {'schema_version': 1, 'classifier_model_sha256': classifier_model_sha256, 'execution_manifest': execution_manifest, 'sources': sources, 'recording_gold_sha256': digest(recording_gold),
            'line_gold_sha256': digest(line_gold), 'enabled_roles': enabled_roles,
            'eligible_ids': sorted(eligible), 'review_cohort': sorted(review_cohort),
            'baseline_commit': BASELINE_COMMIT, 'fixed_commit': FIXED_COMMIT}


def cluster_interval(records, seed=42, iterations=2000):
    """Paired bootstrap of metadata improvement, resampling source groups."""
    groups = {}
    for group, difference in records:
        groups.setdefault(group, []).append(difference)
    if not groups:
        return None
    names = sorted(groups)
    rng = random.Random(seed)
    estimates = []
    for _ in range(iterations):
        sample = [v for name in rng.choices(names, k=len(names)) for v in groups[name]]
        estimates.append(sum(sample) / len(sample))
    estimates.sort()
    return [estimates[int(.025 * iterations)], estimates[min(iterations-1, int(.975 * iterations))]]


def wilson(successes, total):
    if not total:
        return None
    z = 1.959963984540054
    p = successes / total
    d = 1 + z*z/total
    center = (p+z*z/(2*total))/d
    spread = z*math.sqrt(p*(1-p)/total + z*z/(4*total*total))/d
    return [max(0, center-spread), min(1, center+spread)]


def evaluate(protocol, recording_gold, line_gold, arms, line_predictions):
    if protocol.get('schema_version') != 1 or protocol.get('baseline_commit') != BASELINE_COMMIT or protocol.get('fixed_commit') != FIXED_COMMIT:
        raise ValueError('unsupported protocol or application revision')
    if digest(recording_gold) != protocol['recording_gold_sha256'] or digest(line_gold) != protocol['line_gold_sha256']:
        raise ValueError('gold changed after freeze')
    # Revalidate the source-defined cohort rather than trusting edited manifest IDs.
    if freeze_protocol(protocol['sources'], recording_gold, line_gold, protocol['enabled_roles'], classifier_model_sha256=protocol['classifier_model_sha256'], execution_manifest=protocol['execution_manifest']) != protocol:
        raise ValueError('protocol is inconsistent with reviewed sources')
    frozen_hash = digest(protocol)
    sources, gold, lines = indexed(protocol['sources']), indexed(recording_gold), indexed_lines(line_gold)
    eligible = set(protocol['eligible_ids'])
    cohort = set(protocol['review_cohort'])
    if set(arms) != {'baseline', 'fixed', 'assisted'}:
        raise ValueError('three explicit candidate arms required')
    candidates = {}
    for name, arm in arms.items():
        expected_commit = BASELINE_COMMIT if name == 'baseline' else FIXED_COMMIT
        if arm.get('application_commit') != expected_commit:
            raise ValueError('candidate application revision mismatch')
        if name == 'assisted' and arm.get('model_sha256') != protocol['classifier_model_sha256']:
            raise ValueError('assisted model hash mismatch')
        rows = indexed(arm['rows'])
        if set(rows) - set(sources):
            raise ValueError('unexpected candidate recording')
        for case_id, row in rows.items():
            if row.get('freeze_sha256') != frozen_hash or row.get('source_input_sha256') != sources[case_id]['source_input_sha256']:
                raise ValueError('prediction not bound to frozen source protocol')
        candidates[name] = rows
    predictions = indexed_lines(line_predictions)
    if set(predictions) - set(lines):
        raise ValueError('unexpected predicted source line')
    for key, row in predictions.items():
        if row.get('model_sha256') != protocol['classifier_model_sha256'] or row.get('freeze_sha256') != frozen_hash or row.get('source_input_sha256') != sources[key[0]]['source_input_sha256']:
            raise ValueError('line prediction not bound to frozen source protocol')

    def output(name, case_id):
        row = candidates[name].get(case_id, {})
        result = row.get('output')
        if row.get('error') or not isinstance(result, dict) or not valid_metadata(result.get('metadata')) or type(result.get('ready')) is not bool or not all(k in result for k in PARITY_FIELDS):
            return None
        return result

    def complete_track_gold(case_id):
        src, reference = sources[case_id], gold[case_id]
        files = src.get('files')
        return (isinstance(files, list) and bool(files) and len(set(files)) == len(files)
                and isinstance(reference.get('titles'), dict)
                and all(isinstance(v, str) for v in reference['titles'].values())
                and isinstance(reference.get('mappings'), dict) and set(reference['mappings']) == set(files)
                and all(isinstance(m, dict) and type(m.get('disc')) is int and type(m.get('number')) is int
                        and isinstance(m.get('title'), str) for m in reference['mappings'].values()))

    full_gold_ids = {case_id for case_id in eligible if complete_track_gold(case_id)}
    arm_metrics = {}
    for name in candidates:
        correct = ready = false_ready = failures = recording_exact = 0
        for case_id in eligible:
            out = output(name, case_id)
            if out is None:
                failures += 1
                continue
            correct += metadata_equal(out['metadata'], gold[case_id]['metadata'])
            ready += out['ready']
            false_ready += out['ready'] and gold[case_id]['review_required']
            if case_id in full_gold_ids:
                recording_exact += (metadata_equal(out['metadata'], gold[case_id]['metadata'])
                                    and out['ready'] == (not gold[case_id]['review_required'])
                                    and out.get('titles') == gold[case_id]['titles']
                                    and out['mappings'] == gold[case_id]['mappings'])
        arm_metrics[name] = {'eligible': len(eligible), 'metadata_exact': correct, 'ready': ready,
                             'false_ready': false_ready, 'failures': failures, 'full_recording_gold_cases': len(full_gold_ids),
                             'recording_exact': recording_exact}
    parity_failures = []
    new_false_ready = []
    new_material_acceptance = []
    proposal_invalid = []
    proposal_ineligible = []
    proposal_failures = []
    proposed_correct = fixed_correct = 0
    improvements = []
    for case_id in eligible:
        base, fixed, assisted = (output(n, case_id) for n in ('baseline', 'fixed', 'assisted'))
        if fixed is None or assisted is None or any(fixed[k] != assisted[k] for k in PARITY_FIELDS):
            parity_failures.append(case_id)
        if fixed and fixed['ready'] and gold[case_id]['review_required'] and not (base and base['ready']):
            new_false_ready.append(case_id)
        if fixed and fixed['ready']:
            new_wrong_field = any(normalized(fixed['metadata'][f]) != normalized(gold[case_id]['metadata'][f]) and
                                  (not base or not base['ready'] or base['metadata'][f] != fixed['metadata'][f]) for f in FIELDS)
            new_wrong_tracks = any(k in gold[case_id] and fixed.get(k) != gold[case_id][k] and
                                   (not base or not base['ready'] or base.get(k) != fixed.get(k)) for k in ('titles', 'mappings'))
            if new_wrong_field or new_wrong_tracks or case_id in new_false_ready:
                new_material_acceptance.append(case_id)
        merged = dict(fixed['metadata']) if fixed else None
        seen_fields = set()
        proposals = candidates['assisted'].get(case_id, {}).get('proposals', [])
        if not isinstance(proposals, list):
            proposals = [None]
        for p in proposals:
            try:
                if not isinstance(p, dict) or p['field'] not in protocol['enabled_roles'] or p['field'] in seen_fields or not isinstance(p['value'], str):
                    raise ValueError('invalid proposal field')
                seen_fields.add(p['field'])
                key = (case_id, p['note'], p['line_index'])
                prediction = predictions.get(key, {})
                if prediction.get('error') or prediction.get('abstention') or p['field'] not in prediction.get('roles', []):
                    raise ValueError('proposal lacks classifier role evidence')
                source_label = lines[key]
                reference_unknown = source_label['label_status'] not in REVIEWED or source_label['role_disputed']
                if reference_unknown or any(source_label.get(flag, False) for flag in ('mixed_scope', 'uncertainty', 'conflict')):
                    proposal_ineligible.append({'id': case_id, 'field': p['field'], 'note': p['note'],
                                                'line_index': p['line_index'],
                                                'flags': [flag for flag in ('mixed_scope', 'uncertainty', 'conflict') if source_label.get(flag, False)] + (['unresolved_role_reference'] if reference_unknown else [])})
                    merged = None
                    break
                line = source_label['line']
                start, end = p['start'], p['end']
                if type(start) is not int or type(end) is not int or not 0 <= start < end <= len(line) or p['quote'] != line[start:end]:
                    raise ValueError('invalid proposal source span')
                if merged is not None:
                    merged[p['field']] = p['value']
            except (KeyError, TypeError, ValueError):
                proposal_invalid.append(case_id)
                merged = None
                break
        if candidates['assisted'].get(case_id, {}).get('proposal_error'):
            proposal_failures.append(case_id)
            merged = None
        if assisted is None:
            merged = None
        if case_id in cohort:
            f = bool(fixed and metadata_equal(fixed['metadata'], gold[case_id]['metadata']))
            p = metadata_equal(merged, gold[case_id]['metadata'])
            fixed_correct += f
            proposed_correct += p
            improvements.append((sources[case_id]['group'], int(p)-int(f)))

    role_counts = {r: {'tp': 0, 'fp': 0, 'fn': 0, 'recordings': set()} for r in protocol['enabled_roles']}
    background_total = background_fp = line_total = abstentions = disputed = 0
    background_recordings = set()
    affected_recordings = set()
    line_failures = []
    for key, label in lines.items():
        # Role gold and recording gold have independent review statuses.
        line_total += 1
        pred = predictions.get(key)
        emitted = set()
        if pred is None or pred.get('error'):
            line_failures.append(key)
        elif pred.get('abstention'):
            if pred.get('roles'):
                raise ValueError('abstaining prediction must emit no roles')
        elif (not isinstance(pred.get('roles'), list) or any(not isinstance(r, str) for r in pred['roles'])
              or set(pred['roles']) - ROLES or len(set(pred['roles'])) != len(pred['roles'])):
            raise ValueError('invalid role prediction')
        else:
            emitted = set(pred['roles']) & set(protocol['enabled_roles'])
        abstentions += not emitted
        if label['role_disputed'] or label['label_status'] not in REVIEWED:
            disputed += 1
            continue
        truth = set(label['roles'])
        for role, count in role_counts.items():
            if role in emitted:
                count['recordings'].add(key[0])
                count['tp' if role in truth else 'fp'] += 1
            elif role in truth:
                count['fn'] += 1
        if truth and truth <= BACKGROUND:
            background_total += 1
            background_recordings.add(key[0])
            if emitted:
                background_fp += 1
                affected_recordings.add(key[0])
    role_metrics = {}
    for role, c in role_counts.items():
        n = c['tp'] + c['fp']
        role_metrics[role] = {k: c[k] for k in ('tp', 'fp', 'fn')}
        role_metrics[role].update(precision=c['tp']/n if n else None,
                                  recall=c['tp']/(c['tp']+c['fn']) if c['tp']+c['fn'] else None,
                                  precision_wilson95=wilson(c['tp'], n),
                                  supporting_recordings=len(c['recordings']))
    improvement = (proposed_correct-fixed_correct)/len(cohort) if cohort else None
    nonblank_total = sum(bool(r['line'].strip()) for r in lines.values())
    reviewed_nonblank = sum(bool(r['line'].strip()) and r['label_status'] in REVIEWED and not r['role_disputed'] for r in lines.values())
    reference_fraction = reviewed_nonblank/nonblank_total if nonblank_total else None
    gates = {
        'adjudicated_test_support': len(eligible) >= 180,
        'complete_track_reference_coverage': len(full_gold_ids) >= 180 and full_gold_ids == eligible,
        'reviewed_nonblank_reference_coverage': reference_fraction is not None and reference_fraction >= .95,
        'role_precision_and_support': all(c['precision'] is not None and c['precision'] >= .95 and c['supporting_recordings'] >= 20 for c in role_metrics.values()),
        'background_false_positive_rate': background_total > 0 and background_fp/background_total <= .01,
        'metadata_improvement': improvement is not None and improvement >= .05,
        'assistance_parity': not parity_failures,
        'source_backed_proposals': not proposal_invalid,
        'source_eligible_metadata_proposals': not proposal_ineligible,
        'no_new_false_ready': not new_false_ready,
        'no_new_material_acceptance': not new_material_acceptance,
        'complete_candidate_outputs': all(not m['failures'] for m in arm_metrics.values()) and not line_failures and not proposal_failures,
    }
    return {'protocol_sha256': frozen_hash, 'arms': arm_metrics,
            'review_cohort': {'count': len(cohort), 'fixed_metadata_exact': fixed_correct,
                              'proposed_metadata_exact': proposed_correct, 'paired_improvement': improvement,
                              'paired_group_bootstrap95': cluster_interval(improvements)},
            'roles': role_metrics,
            'background': {'lines': background_total, 'false_positive_lines': background_fp,
                           'false_positive_rate': background_fp/background_total if background_total else None,
                           'recordings': len(background_recordings), 'affected_recordings': len(affected_recordings)},
            'reference_coverage': {'requested_recordings': len(sources), 'eligible_recording_gold': len(eligible),
                                   'source_lines': len(lines), 'nonblank_lines': nonblank_total,
                                   'reviewed_nondisputed_nonblank_lines': reviewed_nonblank,
                                   'reviewed_nonblank_fraction': reference_fraction,
                                   'role_metric_scope': 'All requested source recordings with reviewed nondisputed line gold, independent of recording-gold eligibility.'},
            'line_coverage': {'requested_lines': line_total, 'abstentions': abstentions,
                              'disputed_lines': disputed, 'failed_or_missing_lines': len(line_failures)},
            'source_ineligible_proposals': proposal_ineligible, 'proposal_failure_ids': proposal_failures,
            'parity_failure_ids': sorted(parity_failures), 'invalid_proposal_ids': sorted(set(proposal_invalid)),
            'new_false_ready_ids': sorted(new_false_ready), 'new_material_acceptance_ids': sorted(new_material_acceptance), 'gates': gates,
            'quantitative_gates_pass': all(gates.values()),
            'integration_approved': False,
            'remaining_requirements': ['independent accepted-case/source audit', 'runtime performance and artifact checks', 'cross-platform checks', 'integration review'],
            'interval_note': 'Paired improvement resamples recording groups; Wilson role intervals are descriptive line-level intervals, not independent-recording safety guarantees.'}


def main():
    import argparse
    from pathlib import Path
    from .path_guard import require_external

    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    freeze = sub.add_parser('freeze')
    freeze.add_argument('--sources', required=True, help='JSONL source inventories with note line counts')
    freeze.add_argument('--model-sha256', required=True)
    freeze.add_argument('--execution-manifest', required=True)
    freeze.add_argument('--enabled-roles', nargs='+', default=list(FIELDS))
    run = sub.add_parser('evaluate')
    run.add_argument('--protocol', required=True)
    run.add_argument('--arms', required=True, help='JSON object containing three named arms')
    run.add_argument('--line-predictions', required=True, help='JSONL complete line predictions')
    for command in (freeze, run):
        command.add_argument('--recording-gold', required=True)
        command.add_argument('--line-gold', required=True)
        command.add_argument('--output', required=True)
    args = parser.parse_args()

    def read_rows(path):
        return [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()]

    gold, line_gold = read_rows(args.recording_gold), read_rows(args.line_gold)
    if args.command == 'freeze':
        result = freeze_protocol(read_rows(args.sources), gold, line_gold, args.enabled_roles,
                                 classifier_model_sha256=args.model_sha256, execution_manifest=json.loads(Path(args.execution_manifest).read_text()))
    else:
        result = evaluate(json.loads(Path(args.protocol).read_text()), gold, line_gold,
                          json.loads(Path(args.arms).read_text()), read_rows(args.line_predictions))
    encoded = json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + '\n'
    # Never overwrite a frozen protocol or an earlier evaluated result.
    with require_external(args.output).open('x', encoding='utf-8') as stream:
        stream.write(encoded)
    print(json.dumps({'output_sha256': digest(result), 'command': args.command}))


if __name__ == '__main__':
    main()
