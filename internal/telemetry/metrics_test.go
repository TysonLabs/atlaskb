package telemetry

import (
	"testing"
	"time"
)

func resetRegistryForTest() {
	defaultRegistry = &registry{
		counters:   make(map[string]int64),
		histograms: make(map[string]*histogram),
	}
}

func TestCounterHelpers(t *testing.T) {
	resetRegistryForTest()

	AddCounter("jobs", 2)
	IncCounter("jobs")
	AddCounter("other", 5)

	if got := CounterValue("jobs"); got != 3 {
		t.Fatalf("CounterValue(jobs)=%d, want 3", got)
	}
	if got := CounterValue("other"); got != 5 {
		t.Fatalf("CounterValue(other)=%d, want 5", got)
	}
}

func TestObserveDurationAndSnapshot(t *testing.T) {
	resetRegistryForTest()

	ObserveDuration("http", 42*time.Millisecond)
	ObserveDuration("http", -1*time.Millisecond)

	s := SnapshotMetrics()
	h, ok := s.Histograms["http"]
	if !ok {
		t.Fatalf("expected histogram snapshot for http")
	}
	if h.Count != 2 {
		t.Fatalf("histogram count=%d, want 2", h.Count)
	}
	if h.SumMS != 42 {
		t.Fatalf("histogram sum_ms=%d, want 42", h.SumMS)
	}
	if h.Buckets[50] == 0 {
		t.Fatalf("expected <=50ms bucket to increment")
	}

	// Snapshot should be detached from registry maps.
	h.Buckets[50] = 999
	s2 := SnapshotMetrics()
	if s2.Histograms["http"].Buckets[50] == 999 {
		t.Fatalf("snapshot map should not alias internal state")
	}
}

func TestHistogramBucketBoundsCopy(t *testing.T) {
	bounds := HistogramBucketBounds()
	if len(bounds) == 0 {
		t.Fatal("expected non-empty bounds")
	}
	for i := 1; i < len(bounds); i++ {
		if bounds[i] < bounds[i-1] {
			t.Fatalf("bounds not sorted: %v", bounds)
		}
	}

	bounds[0] = -999
	again := HistogramBucketBounds()
	if again[0] == -999 {
		t.Fatal("HistogramBucketBounds should return a copy")
	}
}
