package state

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestTotalElapsed_Basic(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(2 * time.Second)
	t2 := t0.Add(5 * time.Second)
	timing := &Timing{
		entries: []TimingEntry{
			{Phase: "a", Start: t0, End: t1}, // 2s
			{Phase: "b", Start: t0, End: t2}, // 5s
			{Phase: "c", Start: t0},          // zero End — skipped
		},
	}
	got := timing.TotalElapsed()
	want := 7 * time.Second
	if got != want {
		t.Fatalf("TotalElapsed() = %v, want %v", got, want)
	}
}

func TestTotalElapsed_Race(t *testing.T) {
	timing := &Timing{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timing.AddStart("phase")
			timing.AddEnd("phase")
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = timing.TotalElapsed()
		}()
	}
	wg.Wait()
}

func TestAddEndAt(t *testing.T) {
	t0 := time.Now()
	timing := &Timing{
		entries: []TimingEntry{
			{Phase: "a", Start: t0},
		},
	}
	endTime := t0.Add(3 * time.Second)
	timing.AddEndAt("a", endTime)
	if timing.entries[0].End != endTime {
		t.Fatalf("End = %v, want %v", timing.entries[0].End, endTime)
	}
	if timing.entries[0].Duration == "" {
		t.Fatal("Duration should be set")
	}
}

func TestAddStartAt(t *testing.T) {
	startTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	timing := &Timing{}
	timing.AddStartAt("a", startTime)
	if len(timing.entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(timing.entries))
	}
	if timing.entries[0].Start != startTime {
		t.Fatalf("Start = %v, want %v", timing.entries[0].Start, startTime)
	}
	if timing.entries[0].Phase != "a" {
		t.Fatalf("Phase = %q, want %q", timing.entries[0].Phase, "a")
	}
}

func TestNewTiming(t *testing.T) {
	t0 := time.Now()
	entries := []TimingEntry{{Phase: "a", Start: t0}}
	timing := NewTiming(entries)
	got := timing.Entries()
	if len(got) != 1 || got[0].Phase != "a" || got[0].Start != t0 {
		t.Fatalf("NewTiming().Entries() = %v, want matching entries", got)
	}
}

func TestEntries_Snapshot(t *testing.T) {
	t0 := time.Now()
	timing := &Timing{entries: []TimingEntry{{Phase: "x", Start: t0}}}
	snapshot := timing.Entries()
	if len(snapshot) != 1 {
		t.Fatalf("got %d entries, want 1", len(snapshot))
	}
	// Mutate the snapshot — original must be unchanged.
	snapshot[0].Phase = "mutated"
	if timing.entries[0].Phase != "x" {
		t.Fatal("Entries() returned a reference, not a copy")
	}
}

func TestEntries_Race(t *testing.T) {
	timing := &Timing{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timing.AddStart("phase")
			timing.AddEnd("phase")
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = timing.Entries()
		}()
	}
	wg.Wait()
}

func TestTimingJSON_RoundTrip(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Second)
	original := NewTiming([]TimingEntry{
		{Phase: "a", Start: t0, End: t1, Duration: "0m 02s"},
	})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var restored Timing
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := restored.Entries()
	want := original.Entries()
	if len(got) != len(want) || got[0].Phase != want[0].Phase || got[0].Duration != want[0].Duration {
		t.Fatalf("round-trip mismatch: got %v, want %v", got, want)
	}
}

func TestTimingJSON_FlushDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Now()
	timing := NewTiming([]TimingEntry{{Phase: "a", Start: t0, End: t0.Add(time.Second), Duration: "0m 01s"}})
	if err := timing.Flush(dir); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}
