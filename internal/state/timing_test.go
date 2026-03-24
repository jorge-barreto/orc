package state

import (
	"sync"
	"testing"
	"time"
)

func TestTotalElapsed_Basic(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(2 * time.Second)
	t2 := t0.Add(5 * time.Second)
	timing := &Timing{
		Entries: []TimingEntry{
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
		Entries: []TimingEntry{
			{Phase: "a", Start: t0},
		},
	}
	endTime := t0.Add(3 * time.Second)
	timing.AddEndAt("a", endTime)
	if timing.Entries[0].End != endTime {
		t.Fatalf("End = %v, want %v", timing.Entries[0].End, endTime)
	}
	if timing.Entries[0].Duration == "" {
		t.Fatal("Duration should be set")
	}
}

func TestAddStartAt(t *testing.T) {
	startTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	timing := &Timing{}
	timing.AddStartAt("a", startTime)
	if len(timing.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(timing.Entries))
	}
	if timing.Entries[0].Start != startTime {
		t.Fatalf("Start = %v, want %v", timing.Entries[0].Start, startTime)
	}
	if timing.Entries[0].Phase != "a" {
		t.Fatalf("Phase = %q, want %q", timing.Entries[0].Phase, "a")
	}
}
