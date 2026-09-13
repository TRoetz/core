package services

import (
	"math"
	"sort"
	"sync"
	"time"
)

type BulkOperationObservation struct {
	Kind               string
	RequestedItems     int
	ChangedItems       int
	SQLStatements      int
	SideEffectsEmitted int
	PoolInUse          int
	Duration           time.Duration
	Failed             bool
}

type BulkOperationKindStats struct {
	Requests               uint64  `json:"requests"`
	Failures               uint64  `json:"failures"`
	RequestedItems         uint64  `json:"requested_items"`
	ChangedItems           uint64  `json:"changed_items"`
	SQLStatements          uint64  `json:"sql_statements"`
	SideEffectsEmitted     uint64  `json:"side_effects_emitted"`
	LastPoolInUse          int     `json:"last_pool_in_use"`
	PeakObservedPoolInUse  int     `json:"peak_observed_pool_in_use"`
	LatencySamples         int     `json:"latency_samples"`
	LatencyP95Milliseconds float64 `json:"latency_p95_ms"`
	LatencyP99Milliseconds float64 `json:"latency_p99_ms"`
}

type BulkOperationStats struct {
	Operations map[string]BulkOperationKindStats `json:"operations"`
}

type bulkOperationKindMetrics struct {
	stats     BulkOperationKindStats
	latencies []time.Duration
}

type BulkOperationMetrics struct {
	mu    sync.Mutex
	kinds map[string]*bulkOperationKindMetrics
}

func NewBulkOperationMetrics() *BulkOperationMetrics {
	return &BulkOperationMetrics{kinds: map[string]*bulkOperationKindMetrics{}}
}

func (m *BulkOperationMetrics) Stats() BulkOperationStats {
	result := BulkOperationStats{Operations: map[string]BulkOperationKindStats{}}
	if m == nil {
		return result
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, kind := range m.kinds {
		stats := kind.stats
		stats.LatencySamples = len(kind.latencies)
		stats.LatencyP95Milliseconds = percentileMilliseconds(kind.latencies, 0.95)
		stats.LatencyP99Milliseconds = percentileMilliseconds(kind.latencies, 0.99)
		result.Operations[name] = stats
	}
	return result
}

func percentileMilliseconds(samples []time.Duration, percentile float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(float64(len(sorted))*percentile)) - 1
	index = max(0, min(index, len(sorted)-1))
	return float64(sorted[index].Microseconds()) / 1000
}
