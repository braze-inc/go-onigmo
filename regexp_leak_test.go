package onigmo

import (
	"runtime"
	"syscall"
	"testing"
)

// peakRSSBytes returns the process peak resident set size in bytes.
func peakRSSBytes(t *testing.T) int64 {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		return ru.Maxrss // bytes
	}
	return ru.Maxrss * 1024 // KiB
}

const leakLimitBytes = 16 << 20

// Regression: Compile + SearchString + Free must not leak the compiled regex
// or the OnigRegion of a successful match (~1 KiB per iteration on old code).
func TestCompileSearchFreeDoesNotLeak(t *testing.T) {
	run := func(n int) {
		for i := 0; i < n; i++ {
			re, err := Compile("^queue_(?<n>[0-9]+)$")
			if err != nil {
				t.Fatal(err)
			}
			if !re.SearchString("queue_123") {
				t.Fatal("expected a match")
			}
			re.Free()
		}
	}
	run(1000)
	before := peakRSSBytes(t)
	run(200000)
	growth := peakRSSBytes(t) - before
	if growth > leakLimitBytes {
		t.Fatalf("peak RSS grew by %d MiB across 200k compile/search/free cycles", growth>>20)
	}
}

// Regression: repeated successful searches on one compiled regex must release
// the previous match region (~224 B per match on old code).
func TestRepeatedSearchDoesNotLeakRegions(t *testing.T) {
	re, err := Compile("^queue_(?<n>[0-9]+)$")
	if err != nil {
		t.Fatal(err)
	}
	defer re.Free()

	run := func(n int) {
		for i := 0; i < n; i++ {
			if !re.SearchString("queue_123") {
				t.Fatal("expected a match")
			}
			if !re.MatchString("queue_123") {
				t.Fatal("expected a match")
			}
		}
	}
	run(1000)
	before := peakRSSBytes(t)
	run(250000)
	growth := peakRSSBytes(t) - before
	if growth > leakLimitBytes {
		t.Fatalf("peak RSS grew by %d MiB across 500k searches", growth>>20)
	}
}

// Free must be safe on nil receivers and when called repeatedly.
func TestFreeIsNilSafeAndIdempotent(t *testing.T) {
	var nilRe *Regexp
	nilRe.Free()

	re := MustCompile("a+")
	if !re.SearchString("baaa") {
		t.Fatal("expected a match")
	}
	re.matchResult.Free()
	re.matchResult.Free()
	re.Free()
	re.Free()
}
