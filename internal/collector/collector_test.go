package collector

import (
	"testing"
	"time"
)

func TestCalculateWindow(t *testing.T) {
	// 2024-01-01 12:10:30 UTC
	now := time.Date(2024, 1, 1, 12, 10, 30, 0, time.UTC)

	w := CalculateWindow(now, 300, 60)

	// End: truncate(12:10:30 - 300s) = truncate(12:05:30) = 12:05:00
	expectedEnd := time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC)
	// Start: 12:05:00 - 60s = 12:04:00
	expectedStart := time.Date(2024, 1, 1, 12, 4, 0, 0, time.UTC)

	if !w.End.Equal(expectedEnd) {
		t.Fatalf("expected end %v, got %v", expectedEnd, w.End)
	}
	if !w.Start.Equal(expectedStart) {
		t.Fatalf("expected start %v, got %v", expectedStart, w.Start)
	}
}

func TestCalculateWindow_ZeroDelay(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 10, 30, 0, time.UTC)
	w := CalculateWindow(now, 0, 60)

	expectedEnd := time.Date(2024, 1, 1, 12, 10, 0, 0, time.UTC)
	expectedStart := time.Date(2024, 1, 1, 12, 9, 0, 0, time.UTC)

	if !w.End.Equal(expectedEnd) {
		t.Fatalf("expected end %v, got %v", expectedEnd, w.End)
	}
	if !w.Start.Equal(expectedStart) {
		t.Fatalf("expected start %v, got %v", expectedStart, w.Start)
	}
}

func TestCalculateWindow_LargeWindow(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 10, 0, 0, time.UTC)
	w := CalculateWindow(now, 300, 300)

	expectedEnd := time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC)
	expectedStart := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	if !w.End.Equal(expectedEnd) {
		t.Fatalf("expected end %v, got %v", expectedEnd, w.End)
	}
	if !w.Start.Equal(expectedStart) {
		t.Fatalf("expected start %v, got %v", expectedStart, w.Start)
	}
}

func TestCalculateWindow_MinuteAligned(t *testing.T) {
	// Already on a minute boundary
	now := time.Date(2024, 1, 1, 12, 10, 0, 0, time.UTC)
	w := CalculateWindow(now, 300, 60)

	expectedEnd := time.Date(2024, 1, 1, 12, 5, 0, 0, time.UTC)
	if !w.End.Equal(expectedEnd) {
		t.Fatalf("expected end %v, got %v", expectedEnd, w.End)
	}
	if w.End.Second() != 0 || w.Start.Second() != 0 {
		t.Fatal("expected minute-aligned boundaries")
	}
}

func TestTimeWindow_Fields(t *testing.T) {
	tw := TimeWindow{
		Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC),
	}
	if tw.End.Sub(tw.Start) != time.Minute {
		t.Fatal("expected 1 minute window")
	}
}

func TestAPIConstants(t *testing.T) {
	if string(APIGraphQL) != "graphql" {
		t.Fatal("APIGraphQL mismatch")
	}
	if string(APIREST) != "rest" {
		t.Fatal("APIREST mismatch")
	}
}

func TestConstants(t *testing.T) {
	// Verify exported constants match governor values
	if PriorityCritical != 0 {
		t.Fatal("PriorityCritical should be 0")
	}
	if PriorityStandard != 1 {
		t.Fatal("PriorityStandard should be 1")
	}
	if PriorityBackground != 2 {
		t.Fatal("PriorityBackground should be 2")
	}

	if ScopeAccount != "account" {
		t.Fatal("ScopeAccount mismatch")
	}
	if ScopeZone != "zone" {
		t.Fatal("ScopeZone mismatch")
	}
}
