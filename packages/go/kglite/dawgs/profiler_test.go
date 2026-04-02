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
	"sync"
	"testing"
	"time"
)

// helper to ensure profiling is cleaned up after each test
func withProfiling(t *testing.T, fn func()) {
	t.Helper()
	EnableProfiling()
	t.Cleanup(func() { DisableProfiling() })
	fn()
}

func TestProfilerEnableDisable(t *testing.T) {
	// Start clean
	DisableProfiling()
	if ProfilingEnabled() {
		t.Fatal("expected profiling to be disabled initially")
	}

	EnableProfiling()
	if !ProfilingEnabled() {
		t.Fatal("expected profiling to be enabled after EnableProfiling()")
	}

	DisableProfiling()
	if ProfilingEnabled() {
		t.Fatal("expected profiling to be disabled after DisableProfiling()")
	}
}

func TestProfilerRecordQuery(t *testing.T) {
	withProfiling(t, func() {
		recordQuery("MATCH (n) RETURN n", 100*time.Millisecond)

		r := Report()
		if len(r.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(r.Entries))
		}
		if r.Entries[0].Query != "MATCH (n) RETURN n" {
			t.Fatalf("unexpected query: %q", r.Entries[0].Query)
		}
		if r.Entries[0].CallCount != 1 {
			t.Fatalf("expected callCount=1, got %d", r.Entries[0].CallCount)
		}
		if r.Entries[0].TotalTime != 100*time.Millisecond {
			t.Fatalf("expected totalTime=100ms, got %v", r.Entries[0].TotalTime)
		}
	})
}

func TestProfilerMultipleCallsSameQuery(t *testing.T) {
	withProfiling(t, func() {
		durations := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
		for _, d := range durations {
			recordQuery("MATCH (n) RETURN n", d)
		}

		r := Report()
		if len(r.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(r.Entries))
		}
		e := r.Entries[0]
		if e.CallCount != 3 {
			t.Fatalf("expected callCount=3, got %d", e.CallCount)
		}
		expectedTotal := 60 * time.Millisecond
		if e.TotalTime != expectedTotal {
			t.Fatalf("expected totalTime=%v, got %v", expectedTotal, e.TotalTime)
		}
	})
}

func TestProfilerMaxTimeTracking(t *testing.T) {
	withProfiling(t, func() {
		recordQuery("MATCH (n) RETURN n", 10*time.Millisecond)
		recordQuery("MATCH (n) RETURN n", 50*time.Millisecond)
		recordQuery("MATCH (n) RETURN n", 30*time.Millisecond)

		r := Report()
		if len(r.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(r.Entries))
		}
		if r.Entries[0].MaxTime != 50*time.Millisecond {
			t.Fatalf("expected maxTime=50ms, got %v", r.Entries[0].MaxTime)
		}
	})
}

func TestProfilerReportSorting(t *testing.T) {
	withProfiling(t, func() {
		recordQuery("query_small", 10*time.Millisecond)
		recordQuery("query_large", 100*time.Millisecond)
		recordQuery("query_medium", 50*time.Millisecond)

		r := Report()
		if len(r.Entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(r.Entries))
		}
		if r.Entries[0].Query != "query_large" {
			t.Fatalf("expected first entry to be query_large, got %q", r.Entries[0].Query)
		}
		if r.Entries[1].Query != "query_medium" {
			t.Fatalf("expected second entry to be query_medium, got %q", r.Entries[1].Query)
		}
		if r.Entries[2].Query != "query_small" {
			t.Fatalf("expected third entry to be query_small, got %q", r.Entries[2].Query)
		}
	})
}

func TestProfilerReset(t *testing.T) {
	withProfiling(t, func() {
		recordQuery("MATCH (n) RETURN n", 100*time.Millisecond)
		recordQuery("MATCH (m) RETURN m", 200*time.Millisecond)

		Reset()

		r := Report()
		if len(r.Entries) != 0 {
			t.Fatalf("expected 0 entries after Reset(), got %d", len(r.Entries))
		}
		if r.TotalCalls != 0 {
			t.Fatalf("expected TotalCalls=0 after Reset(), got %d", r.TotalCalls)
		}
	})
}

func TestProfilerDisabledIsNoop(t *testing.T) {
	DisableProfiling()
	defer DisableProfiling()

	// recordQuery should not panic when profiling is disabled
	recordQuery("MATCH (n) RETURN n", 100*time.Millisecond)

	r := Report()
	if len(r.Entries) != 0 {
		t.Fatalf("expected 0 entries when profiling disabled, got %d", len(r.Entries))
	}
	if r.TotalCalls != 0 {
		t.Fatalf("expected TotalCalls=0 when profiling disabled, got %d", r.TotalCalls)
	}
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "id equals literal",
			input:    "MATCH (n) WHERE id(n) = 5 RETURN n",
			expected: "MATCH (n) WHERE ? RETURN n",
		},
		{
			name:     "id IN list",
			input:    "MATCH (n) WHERE id(n) IN [1, 2, 3] RETURN n",
			expected: "MATCH (n) WHERE ? RETURN n",
		},
		{
			name:     "parameter reference",
			input:    "MATCH (n) WHERE n.name = $param RETURN n",
			expected: "MATCH (n) WHERE n.name = ? RETURN n",
		},
		{
			name:     "no IDs unchanged",
			input:    "MATCH (n) RETURN n",
			expected: "MATCH (n) RETURN n",
		},
		{
			name:     "bare IN list",
			input:    "MATCH (n) WHERE n.x IN [10, 20, 30] RETURN n",
			expected: "MATCH (n) WHERE n.x ? RETURN n",
		},
		{
			name:     "multiple parameters",
			input:    "MATCH (n) WHERE n.a = $p1 AND n.b = $p2 RETURN n",
			expected: "MATCH (n) WHERE n.a = ? AND n.b = ? RETURN n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeQuery(tc.input)
			if got != tc.expected {
				t.Errorf("normalizeQuery(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestProfilerConcurrentSafety(t *testing.T) {
	withProfiling(t, func() {
		const goroutines = 20
		const queriesPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(goroutines)
		for g := 0; g < goroutines; g++ {
			go func(id int) {
				defer wg.Done()
				for i := 0; i < queriesPerGoroutine; i++ {
					recordQuery("MATCH (n) RETURN n", time.Millisecond)
				}
			}(g)
		}
		wg.Wait()

		r := Report()
		if len(r.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(r.Entries))
		}
		expectedCalls := goroutines * queriesPerGoroutine
		if r.Entries[0].CallCount != expectedCalls {
			t.Fatalf("expected callCount=%d, got %d", expectedCalls, r.Entries[0].CallCount)
		}
		if r.TotalCalls != expectedCalls {
			t.Fatalf("expected TotalCalls=%d, got %d", expectedCalls, r.TotalCalls)
		}
	})
}

func TestProfilerReportTotals(t *testing.T) {
	withProfiling(t, func() {
		recordQuery("query_a", 100*time.Millisecond)
		recordQuery("query_a", 50*time.Millisecond)
		recordQuery("query_b", 200*time.Millisecond)

		r := Report()
		if r.TotalCalls != 3 {
			t.Fatalf("expected TotalCalls=3, got %d", r.TotalCalls)
		}
		expectedTotal := 350 * time.Millisecond
		if r.TotalTime != expectedTotal {
			t.Fatalf("expected TotalTime=%v, got %v", expectedTotal, r.TotalTime)
		}
	})
}

func TestProfilerAvgTime(t *testing.T) {
	withProfiling(t, func() {
		recordQuery("q", 10*time.Millisecond)
		recordQuery("q", 30*time.Millisecond)

		r := Report()
		if len(r.Entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(r.Entries))
		}
		expectedAvg := 20 * time.Millisecond
		if r.Entries[0].AvgTime != expectedAvg {
			t.Fatalf("expected AvgTime=%v, got %v", expectedAvg, r.Entries[0].AvgTime)
		}
	})
}
