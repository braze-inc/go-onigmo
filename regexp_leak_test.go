package onigmo

import (
	"runtime"
	"sync"
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

// leakLimitBytes bounds the RSS growth of the second, measured batch of each
// leak test. The first batch of the same size lets the Go allocator (and the
// race detector's shadow memory, under -race) reach its plateau, so the second
// batch only grows if C memory is leaking per iteration.
const leakLimitBytes = 16 << 20

const leakIterations = 200000

// Regression: Compile + SearchString + Free must not leak the compiled regex
// or the OnigRegion of a successful match (~224 B per iteration on old code).
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
	run(leakIterations)
	before := peakRSSBytes(t)
	run(leakIterations)
	growth := peakRSSBytes(t) - before
	if growth > leakLimitBytes {
		t.Fatalf("peak RSS grew by %d MiB across %d compile/search/free cycles", growth>>20, leakIterations)
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
	run(leakIterations)
	before := peakRSSBytes(t)
	run(leakIterations)
	growth := peakRSSBytes(t) - before
	if growth > leakLimitBytes {
		t.Fatalf("peak RSS grew by %d MiB across %d searches", growth>>20, 2*leakIterations)
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

// Concurrent searches on one Regexp must not double-free the previous match
// region.
func TestConcurrentSearchDoesNotDoubleFree(t *testing.T) {
	re := MustCompile("^queue_(?<n>[0-9]+)$")
	defer re.Free()

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20000; i++ {
				if !re.SearchString("queue_123") {
					t.Error("expected a match")
					return
				}
			}
		}()
	}
	wg.Wait()
}
