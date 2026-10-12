package app

import (
	"context"
	"testing"
	"time"
	"track-tagger/internal/catalog"
)

func TestWatchScheduleUsesObservedStability(t *testing.T) {
	now := time.Unix(100, 0)
	s := watchSchedule{}
	item := Item{Recording: catalog.Recording{ID: "one", Path: "/music/show", Fingerprint: "a"}}
	entry := s.observe(item, now)
	if entry.ready(now.Add(119*time.Second), 2*time.Minute) {
		t.Fatal("premature processing")
	}
	if !entry.ready(now.Add(120*time.Second), 2*time.Minute) {
		t.Fatal("stable recording not ready")
	}
	entry.done = true
	if entry.ready(now.Add(time.Hour), 2*time.Minute) {
		t.Fatal("duplicate processing")
	}
	item.Fingerprint = "b"
	entry = s.observe(item, now.Add(time.Hour))
	if entry.ready(now.Add(time.Hour), 2*time.Minute) {
		t.Fatal("changed inputs must settle")
	}
	item.Reviewed = true
	if got := s.observe(item, now.Add(2*time.Hour)); got == entry {
		t.Fatal("review change did not invalidate observation")
	}
}
func TestWatchEventsResetOnlyContainingRecording(t *testing.T) {
	now := time.Unix(100, 0)
	s := watchSchedule{}
	a := s.observe(Item{Recording: catalog.Recording{ID: "a", Path: "/music/show", Fingerprint: "a"}}, now)
	b := s.observe(Item{Recording: catalog.Recording{ID: "b", Path: "/music/show-other", Fingerprint: "b"}}, now)
	s.changed("/music/show/disc 1/cover.jpg", now.Add(time.Minute))
	if !a.since.Equal(now.Add(time.Minute)) || !b.since.Equal(now) {
		t.Fatal("incorrect recording debounce")
	}
}
func TestWatchManualCorrectionChangesIdentity(t *testing.T) {
	now := time.Unix(100, 0)
	s := watchSchedule{}
	item := Item{Recording: catalog.Recording{ID: "one", Fingerprint: "a", Artist: "First"}, Reviewed: true}
	first := s.observe(item, now)
	item.Artist = "Corrected"
	if first == s.observe(item, now) {
		t.Fatal("manual metadata update was deduplicated")
	}
}
func TestWatchCancellation(t *testing.T) {
	e, err := New(Options{Source: t.TempDir(), State: t.TempDir(), Mode: DryRun})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = e.Watch(ctx, true, time.Hour, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWatchScheduleObservesDateUnknownChange(t *testing.T) {
	now := time.Unix(100, 0)
	s := watchSchedule{}
	item := Item{Recording: catalog.Recording{ID: "one", Path: "/music/show"}}
	first := s.observe(item, now)
	first.done = true
	item.DateUnknown = true
	next := s.observe(item, now.Add(time.Second))
	if next == first || next.done || !next.since.Equal(now.Add(time.Second)) {
		t.Fatal("unknown acknowledgment did not reset observation")
	}
}
