package collector

import (
	"testing"

	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
)

func TestParseDimensionKey(t *testing.T) {
	key := store.MakeDimensionKey("zone_id", "z1", "type", "A")
	parts := parseDimensionKey(key, 2)
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}
	if parts[0] != "zone_id" || parts[1] != "z1" || parts[2] != "type" || parts[3] != "A" {
		t.Fatalf("unexpected parts: %v", parts)
	}
}

func TestParseDimensionKey_WrongCount(t *testing.T) {
	key := store.MakeDimensionKey("zone_id", "z1")
	parts := parseDimensionKey(key, 2)
	if parts != nil {
		t.Fatal("expected nil for wrong pair count")
	}
}

func TestDimValues(t *testing.T) {
	parts := []string{"zone_id", "z1", "type", "A", "code", "NOERROR"}
	values := dimValues(parts)
	if len(values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(values))
	}
	if values[0] != "z1" || values[1] != "A" || values[2] != "NOERROR" {
		t.Fatalf("unexpected values: %v", values)
	}
}

func TestDimValues_Empty(t *testing.T) {
	values := dimValues(nil)
	if len(values) != 0 {
		t.Fatalf("expected 0, got %d", len(values))
	}
}

func TestSplitDimensionKey(t *testing.T) {
	key := store.MakeDimensionKey("a", "1", "b", "2")
	parts := splitDimensionKey(key)
	if len(parts) != 4 {
		t.Fatalf("expected 4, got %d", len(parts))
	}
}
