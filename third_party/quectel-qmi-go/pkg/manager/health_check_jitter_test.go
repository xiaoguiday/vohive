package manager

import (
	"testing"
	"time"
)

// TestJitteredFullCheckIntervalStaysWithinRatioBounds 测试抖动结果始终落在
// base 的 ±fullCheckJitterRatio 范围内，不会失控。
func TestJitteredFullCheckIntervalStaysWithinRatioBounds(t *testing.T) {
	base := 15 * time.Second
	minWant := time.Duration(float64(base) * (1 - fullCheckJitterRatio))
	maxWant := time.Duration(float64(base) * (1 + fullCheckJitterRatio))

	for i := 0; i < 1000; i++ {
		got := jitteredFullCheckInterval(base)
		if got < minWant || got > maxWant {
			t.Fatalf("jitteredFullCheckInterval(%v) = %v, want within [%v, %v]", base, got, minWant, maxWant)
		}
	}
}

// TestJitteredFullCheckIntervalVaries 测试多次调用会产生不同的间隔，
// 这是避免多个 Manager 实例长期锁相的关键——固定值起不到错开的作用。
func TestJitteredFullCheckIntervalVaries(t *testing.T) {
	base := 15 * time.Second
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		seen[jitteredFullCheckInterval(base)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("jitteredFullCheckInterval produced only %d distinct value(s) across 50 calls, want variation", len(seen))
	}
}

// TestJitteredFullCheckIntervalFallsBackToDefaultWhenBaseNonPositive 测试
// base<=0 时回退到 defaultHealthPolicy.FullCheckInterval 而不是产生 0/负值。
func TestJitteredFullCheckIntervalFallsBackToDefaultWhenBaseNonPositive(t *testing.T) {
	got := jitteredFullCheckInterval(0)
	def := defaultHealthPolicy.FullCheckInterval
	minWant := time.Duration(float64(def) * (1 - fullCheckJitterRatio))
	maxWant := time.Duration(float64(def) * (1 + fullCheckJitterRatio))
	if got < minWant || got > maxWant {
		t.Fatalf("jitteredFullCheckInterval(0) = %v, want within [%v, %v] (default-based)", got, minWant, maxWant)
	}
}
