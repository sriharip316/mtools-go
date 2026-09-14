package load

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// OpStats tracks latency and count for a specific operation type.
type OpStats struct {
	Count    int64
	TotalLat time.Duration
	MinLat   time.Duration
	MaxLat   time.Duration
	Samples  []time.Duration
}

func newOpStats() *OpStats {
	return &OpStats{
		Samples: make([]time.Duration, 0, 1000),
	}
}

func (s *OpStats) Record(lat time.Duration) {
	s.Count++
	s.TotalLat += lat
	if s.MinLat == 0 || lat < s.MinLat {
		s.MinLat = lat
	}
	if lat > s.MaxLat {
		s.MaxLat = lat
	}
	if len(s.Samples) < 5000 {
		s.Samples = append(s.Samples, lat)
	} else {
		// Reservoir replacement
		idx := int(s.Count % 5000)
		s.Samples[idx] = lat
	}
}

func (s *OpStats) Avg() time.Duration {
	if s.Count == 0 {
		return 0
	}
	return time.Duration(int64(s.TotalLat) / s.Count)
}

func (s *OpStats) Percentile(p float64) time.Duration {
	if len(s.Samples) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(s.Samples))
	copy(sorted, s.Samples)
	slices.Sort(sorted)

	idx := max(int(float64(len(sorted)-1)*(p/100.0)), 0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// StatsTracker aggregates real-time and cumulative simulation statistics.
type StatsTracker struct {
	startTime  time.Time
	isPhase2   bool
	phase2Time time.Time

	mu sync.Mutex

	// Counters
	initLoadDocs  int64
	totalOps      int64
	createOps     int64
	readHits      int64
	readMisses    int64
	updateOps     int64
	deleteOps     int64
	errorCount    int64
	lastError     string
	collectionOps map[string]int64

	// Latencies
	initLoadLat *OpStats
	createLat   *OpStats
	readLat     *OpStats
	updateLat   *OpStats
	deleteLat   *OpStats

	// Snapshot delta tracking
	lastTotalOps  int64
	lastCheckTime time.Time
}

// NewStatsTracker creates a new statistics tracker.
func NewStatsTracker() *StatsTracker {
	now := time.Now()
	return &StatsTracker{
		startTime:     now,
		lastCheckTime: now,
		collectionOps: make(map[string]int64),
		initLoadLat:   newOpStats(),
		createLat:     newOpStats(),
		readLat:       newOpStats(),
		updateLat:     newOpStats(),
		deleteLat:     newOpStats(),
	}
}

// SetPhase2 marks the transition from Initial Load to CRUD simulation.
func (st *StatsTracker) SetPhase2() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.isPhase2 = true
	st.phase2Time = time.Now()
	st.lastTotalOps = st.totalOps
	st.lastCheckTime = time.Now()
}

// RecordInitLoad records a document insertion during Phase 1.
func (st *StatsTracker) RecordInitLoad(coll string, lat time.Duration, count int64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.initLoadDocs += count
	st.totalOps += count
	st.collectionOps[coll] += count
	for range count {
		st.initLoadLat.Record(lat)
	}
}

// RecordCRUD records a CRUD operation during Phase 2.
func (st *StatsTracker) RecordCRUD(coll string, op OpType, lat time.Duration, hit bool, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.totalOps++
	st.collectionOps[coll]++

	if err != nil {
		st.errorCount++
		st.lastError = err.Error()
		return
	}

	switch op {
	case OpCreate:
		st.createOps++
		st.createLat.Record(lat)
	case OpRead:
		if hit {
			st.readHits++
		} else {
			st.readMisses++
		}
		st.readLat.Record(lat)
	case OpUpdate:
		st.updateOps++
		st.updateLat.Record(lat)
	case OpDelete:
		st.deleteOps++
		st.deleteLat.Record(lat)
	}
}

// RecordError increments the error counter.
func (st *StatsTracker) RecordError(err error) {
	if err == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.errorCount++
	st.lastError = err.Error()
}

// IntervalReport prints a live periodic stats line to the terminal.
func (st *StatsTracker) IntervalReport(targetRate float64, jsonOut bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	elapsedTotal := now.Sub(st.startTime)
	windowElapsed := now.Sub(st.lastCheckTime).Seconds()
	if windowElapsed <= 0 {
		windowElapsed = 1.0
	}

	deltaOps := st.totalOps - st.lastTotalOps
	instantRate := float64(deltaOps) / windowElapsed

	st.lastTotalOps = st.totalOps
	st.lastCheckTime = now

	elapsedStr := formatElapsed(elapsedTotal)

	if jsonOut {
		data := map[string]any{
			"elapsed":     elapsedTotal.String(),
			"phase":       map[bool]string{false: "INIT_LOAD", true: "CRUD"}[st.isPhase2],
			"rate":        instantRate,
			"target_rate": targetRate,
			"total_ops":   st.totalOps,
			"init_docs":   st.initLoadDocs,
			"create_ops":  st.createOps,
			"read_hits":   st.readHits,
			"read_misses": st.readMisses,
			"update_ops":  st.updateOps,
			"delete_ops":  st.deleteOps,
			"errors":      st.errorCount,
		}
		jsonBytes, _ := json.Marshal(data)
		fmt.Println(string(jsonBytes))
		return
	}

	if !st.isPhase2 {
		// Phase 1 Initial Load formatting
		var collParts []string
		for coll, count := range st.collectionOps {
			collParts = append(collParts, fmt.Sprintf("%s: %d", coll, count))
		}
		sort.Strings(collParts)

		fmt.Printf("[%s] [INIT LOAD] Rate: %.1f docs/s | Total: %d docs (%s) | Latency avg: %s | Errors: %d\n",
			elapsedStr,
			instantRate,
			st.initLoadDocs,
			strings.Join(collParts, ", "),
			formatDuration(st.initLoadLat.Avg()),
			st.errorCount,
		)
	} else {
		// Phase 2 CRUD formatting
		totalReads := st.readHits + st.readMisses
		fmt.Printf("[%s] [CRUD] Rate: %.1f ops/s (target: %.1f/s) | Total: %d ops (C: %d, R: %d [hit: %d, miss: %d], U: %d, D: %d) | Latency avg: %s (R: %s, U: %s) | Errors: %d\n",
			elapsedStr,
			instantRate,
			targetRate,
			st.totalOps,
			st.createOps,
			totalReads,
			st.readHits,
			st.readMisses,
			st.updateOps,
			st.deleteOps,
			formatDuration(st.overallAvgLatency()),
			formatDuration(st.readLat.Avg()),
			formatDuration(st.updateLat.Avg()),
			st.errorCount,
		)
	}
}

func (st *StatsTracker) overallAvgLatency() time.Duration {
	totalCount := st.createLat.Count + st.readLat.Count + st.updateLat.Count + st.deleteLat.Count
	if totalCount == 0 {
		return 0
	}
	totalLat := st.createLat.TotalLat + st.readLat.TotalLat + st.updateLat.TotalLat + st.deleteLat.TotalLat
	return time.Duration(int64(totalLat) / totalCount)
}

// PrintSummary outputs the final summary report table.
func (st *StatsTracker) PrintSummary() {
	st.mu.Lock()
	defer st.mu.Unlock()

	totalDur := time.Since(st.startTime)
	overallThroughput := 0.0
	if totalDur.Seconds() > 0 {
		overallThroughput = float64(st.totalOps) / totalDur.Seconds()
	}

	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println("                         mtools load simulation summary                         ")
	fmt.Println("================================================================================")
	fmt.Printf("  Total Duration:        %s\n", totalDur.Round(time.Millisecond))
	fmt.Printf("  Overall Operations:    %d ops (%.1f ops/s)\n", st.totalOps, overallThroughput)
	fmt.Printf("  Initial Load Seed:     %d documents\n", st.initLoadDocs)
	fmt.Printf("  Errors Encountered:    %d\n", st.errorCount)
	if st.errorCount > 0 && st.lastError != "" {
		fmt.Printf("  Last Error:            %s\n", st.lastError)
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("  Operation Breakdown:")
	totalReads := st.readHits + st.readMisses
	readHitPct := 0.0
	if totalReads > 0 {
		readHitPct = (float64(st.readHits) / float64(totalReads)) * 100.0
	}
	fmt.Printf("    - Create (Phase 2):  %d ops\n", st.createOps)
	fmt.Printf("    - Read:              %d ops (hits: %d [%.1f%%], misses: %d)\n", totalReads, st.readHits, readHitPct, st.readMisses)
	fmt.Printf("    - Update:            %d ops\n", st.updateOps)
	fmt.Printf("    - Delete:            %d ops\n", st.deleteOps)

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("  Collection Breakdown:")
	var collNames []string
	for name := range st.collectionOps {
		collNames = append(collNames, name)
	}
	sort.Strings(collNames)
	for _, name := range collNames {
		count := st.collectionOps[name]
		pct := 0.0
		if st.totalOps > 0 {
			pct = (float64(count) / float64(st.totalOps)) * 100.0
		}
		fmt.Printf("    - %-18s %d ops (%.1f%%)\n", name+":", count, pct)
	}

	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("  Latency Metrics (ms):")
	fmt.Println("    OP          COUNT        AVG        MIN        P50        P95        MAX")
	printLatencyRow("Init Load", st.initLoadLat)
	printLatencyRow("Create", st.createLat)
	printLatencyRow("Read", st.readLat)
	printLatencyRow("Update", st.updateLat)
	printLatencyRow("Delete", st.deleteLat)
	fmt.Println("================================================================================")
}

func printLatencyRow(label string, s *OpStats) {
	if s.Count == 0 {
		return
	}
	fmt.Printf("    %-11s %-12d %-10.2f %-10.2f %-10.2f %-10.2f %-10.2f\n",
		label,
		s.Count,
		toMs(s.Avg()),
		toMs(s.MinLat),
		toMs(s.Percentile(50)),
		toMs(s.Percentile(95)),
		toMs(s.MaxLat),
	)
}

func toMs(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
}

func formatElapsed(d time.Duration) string {
	d = d.Truncate(time.Second)
	totalSec := int(d.Seconds())
	hr := totalSec / 3600
	min := (totalSec % 3600) / 60
	sec := totalSec % 60
	if hr > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hr, min, sec)
	}
	return fmt.Sprintf("%02d:%02d", min, sec)
}
