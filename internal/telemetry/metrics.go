package telemetry

import (
	"sort"
	"sync"
	"time"
)

type HistogramSnapshot struct {
	Count   int64         `json:"count"`
	SumMS   int64         `json:"sum_ms"`
	Buckets map[int]int64 `json:"buckets"`
}

type Snapshot struct {
	Counters  map[string]int64               `json:"counters"`
	Histograms map[string]HistogramSnapshot  `json:"histograms"`
}

type histogram struct {
	count   int64
	sumMS   int64
	buckets map[int]int64
}

type registry struct {
	mu         sync.Mutex
	counters   map[string]int64
	histograms map[string]*histogram
}

var defaultRegistry = &registry{
	counters:   make(map[string]int64),
	histograms: make(map[string]*histogram),
}

var defaultBucketsMS = []int{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

func AddCounter(name string, delta int64) {
	defaultRegistry.mu.Lock()
	defaultRegistry.counters[name] += delta
	defaultRegistry.mu.Unlock()
}

func IncCounter(name string) {
	AddCounter(name, 1)
}

func ObserveDuration(name string, d time.Duration) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()

	h, ok := defaultRegistry.histograms[name]
	if !ok {
		h = &histogram{buckets: make(map[int]int64)}
		defaultRegistry.histograms[name] = h
	}

	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	h.count++
	h.sumMS += ms
	for _, upper := range defaultBucketsMS {
		if ms <= int64(upper) {
			h.buckets[upper]++
		}
	}
}

func SnapshotMetrics() Snapshot {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()

	out := Snapshot{
		Counters:   make(map[string]int64, len(defaultRegistry.counters)),
		Histograms: make(map[string]HistogramSnapshot, len(defaultRegistry.histograms)),
	}
	for k, v := range defaultRegistry.counters {
		out.Counters[k] = v
	}
	for name, h := range defaultRegistry.histograms {
		bs := make(map[int]int64, len(h.buckets))
		for b, c := range h.buckets {
			bs[b] = c
		}
		out.Histograms[name] = HistogramSnapshot{
			Count:   h.count,
			SumMS:   h.sumMS,
			Buckets: bs,
		}
	}
	return out
}

func CounterValue(name string) int64 {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	return defaultRegistry.counters[name]
}

func HistogramBucketBounds() []int {
	b := make([]int, len(defaultBucketsMS))
	copy(b, defaultBucketsMS)
	sort.Ints(b)
	return b
}
