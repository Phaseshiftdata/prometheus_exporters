package collector

import (
	"strings"

	"github.com/asymmetric-effort/prometheus-exporters/internal/store"
)

// parseDimensionKey splits a DimensionKey back into its component parts.
// The key is expected to contain alternating name-value pairs separated by
// null bytes. expectedPairs is the number of name-value pairs expected.
// Returns the full slice of parts (name1, value1, name2, value2, ...) or nil
// if the count does not match.
func parseDimensionKey(key store.DimensionKey, expectedPairs int) []string {
	parts := strings.Split(string(key), "\x00")
	if len(parts) != expectedPairs*2 {
		return nil
	}
	return parts
}

// dimValues extracts only the values (odd-indexed elements) from a parsed
// dimension key slice. This is useful when passing label values to prometheus
// metric constructors where the label names are already defined in the Desc.
func dimValues(parts []string) []string {
	values := make([]string, 0, len(parts)/2)
	for i := 1; i < len(parts); i += 2 {
		values = append(values, parts[i])
	}
	return values
}
