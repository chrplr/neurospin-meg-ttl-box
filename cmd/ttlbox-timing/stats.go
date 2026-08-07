// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package main

import (
	"fmt"
	"slices"
	"time"
)

// summary describes a distribution of durations.
//
// The percentiles go to 99.9 because a trigger-timing figure is not usefully
// summarised by its median: an experiment is corrupted by the trials in the
// tail, and a mean latency of 1.5 ms with a 20 ms worst case is a different
// device from one with a 3 ms worst case.
type summary struct {
	N                        int
	Min, P50, P95, P99, P999 time.Duration
	Max, Mean                time.Duration
}

func summarise(d []time.Duration) summary {
	if len(d) == 0 {
		return summary{}
	}
	s := slices.Clone(d)
	slices.Sort(s)
	var total time.Duration
	for _, v := range s {
		total += v
	}
	return summary{
		N:    len(s),
		Min:  s[0],
		P50:  quantile(s, 0.50),
		P95:  quantile(s, 0.95),
		P99:  quantile(s, 0.99),
		P999: quantile(s, 0.999),
		Max:  s[len(s)-1],
		Mean: total / time.Duration(len(s)),
	}
}

// quantile returns the nearest-rank quantile of an already sorted slice. The
// nearest rank is deliberate: it reports a value that was actually observed,
// which is what a claim about a worst case should rest on.
func quantile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(q*float64(len(sorted))+0.5) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

// String renders a summary in microseconds, the unit everything here is
// measured in.
func (s summary) String() string {
	if s.N == 0 {
		return "no data"
	}
	return fmt.Sprintf("n=%d  min=%v  p50=%v  p95=%v  p99=%v  p99.9=%v  max=%v  mean=%v",
		s.N, s.Min, s.P50, s.P95, s.P99, s.P999, s.Max, s.Mean)
}
