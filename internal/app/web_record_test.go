package app

import (
	"testing"

	"track-tagger/internal/catalog"
)

func TestWebRecordUsesLatestCachedEvidenceTime(t *testing.T) {
	r := Item{Recording: catalog.Recording{ID: "one", Evidence: []catalog.Evidence{{ModTime: 1700000000000000001}, {ModTime: 1700000000000000003}, {ModTime: 1700000000000000002}}}}
	if got := webRecord(r).LastModified; got != 1700000000000000003 {
		t.Fatalf("last modified = %d", got)
	}
	if got := webRecord(Item{}).LastModified; got != 0 {
		t.Fatalf("missing evidence time = %d", got)
	}
}
