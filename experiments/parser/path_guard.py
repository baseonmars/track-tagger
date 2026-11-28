"""Keep source text and model downloads outside every Git checkout/worktree."""
from pathlib import Path

def require_external(path):
    path=Path(path).resolve()
    if any((parent/'.git').exists() for parent in (path,*path.parents)):
        raise ValueError('external output must stay outside Git checkouts')
    return path
