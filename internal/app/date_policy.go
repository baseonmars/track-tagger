package app

import "track-tagger/internal/catalog"

func (e *Engine) DatePolicy() catalog.DatePolicy {
	if e.reviewAmbiguousDates.Load() {
		return catalog.ReviewAmbiguous
	}
	return catalog.AssumeUS
}

func (w WebBackend) UseUSDates() bool { return w.E.DatePolicy() == catalog.AssumeUS }

func (w WebBackend) SetUSDates(enabled bool) {
	e := w.E
	e.validationMu.Lock()
	defer e.validationMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	reviewRequired := !enabled
	if e.reviewAmbiguousDates.Swap(reviewRequired) != reviewRequired {
		e.invalidateLibrary()
	}
}
