// Copyright 2025 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package dawgs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var reNormalizeIDs = regexp.MustCompile(`(?:id\(\w+\)\s*(?:IN\s*\[[0-9, ]+\]|=\s*\$?\w+))|(?:IN\s*\[[0-9, ]+\])|(?:\$\w+)`)

// QueryProfiler collects per-query timing statistics for kglite Cypher execution.
// Enable it by calling EnableProfiling(). Results are retrieved via Report().
// All methods are goroutine-safe.
type QueryProfiler struct {
	mu      sync.Mutex
	entries map[string]*queryStats
}

type queryStats struct {
	callCount int
	totalTime time.Duration
	maxTime   time.Duration
}

var globalProfiler *QueryProfiler

// EnableProfiling activates query-level timing collection.
func EnableProfiling() {
	globalProfiler = &QueryProfiler{
		entries: make(map[string]*queryStats),
	}
}

// DisableProfiling turns off profiling and discards collected data.
func DisableProfiling() {
	globalProfiler = nil
}

// ProfilingEnabled returns true if the global profiler is active.
func ProfilingEnabled() bool {
	return globalProfiler != nil
}

// recordQuery records a single query execution. Called from Transaction.Raw and Batch.flush.
func recordQuery(cypher string, elapsed time.Duration) {
	p := globalProfiler
	if p == nil {
		return
	}

	// Normalize the query by trimming whitespace for grouping
	key := strings.TrimSpace(cypher)

	p.mu.Lock()
	defer p.mu.Unlock()

	s, ok := p.entries[key]
	if !ok {
		s = &queryStats{}
		p.entries[key] = s
	}
	s.callCount++
	s.totalTime += elapsed
	if elapsed > s.maxTime {
		s.maxTime = elapsed
	}
}

// ProfileReport contains the profiling results.
type ProfileReport struct {
	Entries    []ProfileEntry
	TotalTime time.Duration
	TotalCalls int
}

type ProfileEntry struct {
	Query     string
	CallCount int
	TotalTime time.Duration
	MaxTime   time.Duration
	AvgTime   time.Duration
}

// Report returns a summary of all recorded queries, sorted by total time descending.
func Report() *ProfileReport {
	p := globalProfiler
	if p == nil {
		return &ProfileReport{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	report := &ProfileReport{
		Entries: make([]ProfileEntry, 0, len(p.entries)),
	}

	for q, s := range p.entries {
		avg := s.totalTime / time.Duration(s.callCount)
		entry := ProfileEntry{
			Query:     q,
			CallCount: s.callCount,
			TotalTime: s.totalTime,
			MaxTime:   s.maxTime,
			AvgTime:   avg,
		}
		report.Entries = append(report.Entries, entry)
		report.TotalTime += s.totalTime
		report.TotalCalls += s.callCount
	}

	sort.Slice(report.Entries, func(i, j int) bool {
		return report.Entries[i].TotalTime > report.Entries[j].TotalTime
	})

	return report
}

// PrintReport prints a human-readable profiling report to stdout.
func PrintReport() {
	r := Report()
	if len(r.Entries) == 0 {
		fmt.Println("KGLITE PROFILER: no queries recorded")
		return
	}

	fmt.Printf("\n=== KGLITE QUERY PROFILE ===\n")
	fmt.Printf("Total queries: %d | Total time: %v\n\n", r.TotalCalls, r.TotalTime)

	// Show top 50 by total time
	limit := 50
	if len(r.Entries) < limit {
		limit = len(r.Entries)
	}

	for i := 0; i < limit; i++ {
		e := r.Entries[i]
		pct := float64(e.TotalTime) / float64(r.TotalTime) * 100
		// Truncate query for display
		q := e.Query
		if len(q) > 200 {
			q = q[:200] + "..."
		}
		fmt.Printf("#%d  %.1f%%  total=%v  calls=%d  avg=%v  max=%v\n    %s\n\n",
			i+1, pct, e.TotalTime, e.CallCount, e.AvgTime, e.MaxTime, q)
	}

	if len(r.Entries) > limit {
		// Summarize remaining
		var remainTime time.Duration
		var remainCalls int
		for i := limit; i < len(r.Entries); i++ {
			remainTime += r.Entries[i].TotalTime
			remainCalls += r.Entries[i].CallCount
		}
		fmt.Printf("... and %d more queries (total=%v, calls=%d)\n", len(r.Entries)-limit, remainTime, remainCalls)
	}
	// Aggregate by normalized pattern (replace literal IDs with placeholders)
	patternStats := make(map[string]*queryStats)
	for _, e := range r.Entries {
		normalized := normalizeQuery(e.Query)
		s, ok := patternStats[normalized]
		if !ok {
			s = &queryStats{}
			patternStats[normalized] = s
		}
		s.callCount += e.CallCount
		s.totalTime += e.TotalTime
		if e.MaxTime > s.maxTime {
			s.maxTime = e.MaxTime
		}
	}

	type patternEntry struct {
		pattern string
		stats   *queryStats
	}
	var patterns []patternEntry
	for p, s := range patternStats {
		patterns = append(patterns, patternEntry{p, s})
	}
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].stats.totalTime > patterns[j].stats.totalTime
	})

	fmt.Printf("\n--- Aggregated by query pattern ---\n\n")
	for i, pe := range patterns {
		if i >= 30 {
			break
		}
		pct := float64(pe.stats.totalTime) / float64(r.TotalTime) * 100
		q := pe.pattern
		if len(q) > 200 {
			q = q[:200] + "..."
		}
		avg := pe.stats.totalTime / time.Duration(pe.stats.callCount)
		fmt.Printf("#%d  %.1f%%  total=%v  calls=%d  avg=%v  max=%v\n    %s\n\n",
			i+1, pct, pe.stats.totalTime, pe.stats.callCount, avg, pe.stats.maxTime, q)
	}

	fmt.Printf("=== END PROFILE ===\n\n")
}

// normalizeQuery replaces literal IDs and parameter references with placeholders
// so that structurally identical queries can be grouped together.
func normalizeQuery(q string) string {
	return reNormalizeIDs.ReplaceAllString(q, "?")
}

// Reset clears all collected profiling data without disabling profiling.
func Reset() {
	p := globalProfiler
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make(map[string]*queryStats)
}
