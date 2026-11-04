"""Real candidate inference requires immutable reviewed labels before dispatch."""

def candidate_guard(case,phase,baseline=False):
    if phase.startswith('annotation-'):
        return
    if case.get('split') not in ('validation','test'):
        return
    if case.get('provenance',{}).get('kind')=='synthetic':
        return
    if not case.get('_labels_frozen_sha256'):
        raise ValueError('real candidate predictions require reviewed frozen labels')
    if case.get('label_status') not in ('adjudicated','agent-reviewed','disputed','unresolved'):
        raise ValueError('real candidate case has not completed source annotation review')
