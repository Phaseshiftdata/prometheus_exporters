package store

import (
	"sync"
	"testing"
	"time"
)

func TestAdd_NewEntry(t *testing.T) {
	s := NewStore(10 * time.Minute)
	dims := MakeDimensionKey("zone", "z1", "type", "A")
	ok := s.Add("dns_queries", dims, time.Now(), 100)
	if !ok {
		t.Fatal("expected Add to return true for new entry")
	}
	if got := s.Get("dns_queries", dims); got != 100 {
		t.Fatalf("expected 100, got %f", got)
	}
}

func TestAdd_DuplicateEntry(t *testing.T) {
	s := NewStore(10 * time.Minute)
	dims := MakeDimensionKey("zone", "z1")
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ok1 := s.Add("m", dims, ts, 50)
	ok2 := s.Add("m", dims, ts, 50)
	if !ok1 {
		t.Fatal("first add should return true")
	}
	if ok2 {
		t.Fatal("duplicate add should return false")
	}
	if got := s.Get("m", dims); got != 50 {
		t.Fatalf("expected 50 (no double-count), got %f", got)
	}
}

func TestAdd_AccumulatesDifferentTimeBuckets(t *testing.T) {
	s := NewStore(10 * time.Minute)
	dims := MakeDimensionKey("zone", "z1")
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC)

	s.Add("m", dims, t1, 10)
	s.Add("m", dims, t2, 20)

	if got := s.Get("m", dims); got != 30 {
		t.Fatalf("expected 30, got %f", got)
	}
}

func TestGet_NonExistent(t *testing.T) {
	s := NewStore(10 * time.Minute)
	if got := s.Get("nonexistent", "dims"); got != 0 {
		t.Fatalf("expected 0, got %f", got)
	}
}

func TestGetAll(t *testing.T) {
	s := NewStore(10 * time.Minute)
	ts := time.Now()
	d1 := MakeDimensionKey("a", "1")
	d2 := MakeDimensionKey("a", "2")

	s.Add("m", d1, ts, 10)
	s.Add("m", d2, ts, 20)

	all := s.GetAll("m")
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all[d1] != 10 || all[d2] != 20 {
		t.Fatalf("unexpected values: %v", all)
	}
}

func TestGetAll_Empty(t *testing.T) {
	s := NewStore(10 * time.Minute)
	all := s.GetAll("nonexistent")
	if all == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(all) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(all))
	}
}

func TestGetAll_ReturnsShallowCopy(t *testing.T) {
	s := NewStore(10 * time.Minute)
	d1 := MakeDimensionKey("a", "1")
	s.Add("m", d1, time.Now(), 10)

	all := s.GetAll("m")
	all[d1] = 999 // mutate the copy

	if got := s.Get("m", d1); got != 10 {
		t.Fatal("GetAll returned reference, not a copy")
	}
}

func TestPrune(t *testing.T) {
	s := NewStore(1 * time.Millisecond)
	dims := MakeDimensionKey("a", "1")
	ts := time.Now()

	s.Add("m", dims, ts, 10)

	// Wait for entries to become pruneable
	time.Sleep(5 * time.Millisecond)
	s.Prune()

	// The counter should still exist, only the dedup record is pruned
	if got := s.Get("m", dims); got != 10 {
		t.Fatalf("counter should still exist after prune, got %f", got)
	}

	// Now the same bucket can be re-added (dedup record was pruned)
	ok := s.Add("m", dims, ts, 10)
	if !ok {
		t.Fatal("expected Add to succeed after prune removed dedup record")
	}
	// Counter is now doubled
	if got := s.Get("m", dims); got != 20 {
		t.Fatalf("expected 20 after re-add, got %f", got)
	}
}

func TestReset(t *testing.T) {
	s := NewStore(10 * time.Minute)
	dims := MakeDimensionKey("a", "1")
	s.Add("m", dims, time.Now(), 100)

	s.Reset()

	if got := s.Get("m", dims); got != 0 {
		t.Fatalf("expected 0 after reset, got %f", got)
	}
	// Should be able to re-add after reset
	ok := s.Add("m", dims, time.Now(), 50)
	if !ok {
		t.Fatal("expected Add to succeed after reset")
	}
}

func TestMakeDimensionKey(t *testing.T) {
	key := MakeDimensionKey("zone", "z1", "type", "A")
	expected := DimensionKey("zone\x00z1\x00type\x00A")
	if key != expected {
		t.Fatalf("expected %q, got %q", expected, key)
	}
}

func TestMakeDimensionKey_Empty(t *testing.T) {
	key := MakeDimensionKey()
	if key != "" {
		t.Fatalf("expected empty key, got %q", key)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := NewStore(10 * time.Minute)
	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			dims := MakeDimensionKey("g", "1")
			for i := 0; i < opsPerGoroutine; i++ {
				// Use nanoseconds to ensure uniqueness: id*opsPerGoroutine+i
				unique := id*opsPerGoroutine + i
				ts := time.Unix(int64(unique), 0)
				s.Add("m", dims, ts, 1)
				s.Get("m", dims)
				s.GetAll("m")
			}
		}(g)
	}

	wg.Wait()

	// The value should be exactly goroutines * opsPerGoroutine since each
	// (goroutine_id, i) pair is unique.
	got := s.Get("m", MakeDimensionKey("g", "1"))
	if got != goroutines*opsPerGoroutine {
		t.Fatalf("expected %d, got %f", goroutines*opsPerGoroutine, got)
	}
}

func TestConcurrentPrune(t *testing.T) {
	s := NewStore(1 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			dims := MakeDimensionKey("a", "1")
			s.Add("m", dims, time.Date(2024, 1, 1, 0, 0, i, 0, time.UTC), 1)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.Prune()
		}
	}()

	wg.Wait()
}
