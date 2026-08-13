package collector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"github.com/phaseshiftdata/prometheus_exporters/internal/store"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// prohibitedDimensionNames are NFR-3 privacy-sensitive dimensions that must
// never appear in any metric label.
var prohibitedDimensionNames = []string{
	"userEmail",
	"userUID",
	"deviceIdentifier",
	"sourceIP",
	"destinationIP",
	"clientIP",
	"deviceId",
	"email",
	"userIdentifier",
	"deviceSerialNumber",
}

// TestGolden_NoProhibitedDimensions asserts that no prohibited dimension names
// appear as label names in any metric output from any collector.
func TestGolden_NoProhibitedDimensions(t *testing.T) {
	ts := newTestSetup(t)

	// Create a dummy server for collectors that need one
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(map[string]interface{}{
			"viewer": map[string]interface{}{
				"accounts": []interface{}{},
				"zones":    []interface{}{},
			},
		}))
	}))
	defer server.Close()
	client := createTestClient(server)

	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	// Register all collectors with the same registry
	NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, ts.registry)

	reg2 := prometheus.NewRegistry()
	NewGatewayDNSCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, reg2)

	reg3 := prometheus.NewRegistry()
	NewGatewayNetworkCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, reg3)

	reg4 := prometheus.NewRegistry()
	NewBrowserIsolationCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, reg4)

	reg5 := prometheus.NewRegistry()
	NewTunnelCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, reg5)

	reg6 := prometheus.NewRegistry()
	NewDNSCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, reg6)

	reg7 := prometheus.NewRegistry()
	NewDNSFirewallCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, reg7)

	reg8 := prometheus.NewRegistry()
	NewDomainCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, nil, reg8)

	reg9 := prometheus.NewRegistry()
	NewCertificateCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, reg9)

	reg10 := prometheus.NewRegistry()
	NewZoneStatusCollector(client, store.NewStore(10*time.Minute), ts.selfMetrics, ts.logger, 300, 60, 60, []string{"acc1"}, zones, nil, reg10)

	// Gather metrics from all registries
	registries := []*prometheus.Registry{ts.registry, reg2, reg3, reg4, reg5, reg6, reg7, reg8, reg9, reg10}

	for _, reg := range registries {
		families, err := reg.Gather()
		if err != nil {
			t.Fatalf("Gather failed: %v", err)
		}

		for _, f := range families {
			for _, m := range f.GetMetric() {
				for _, lp := range m.GetLabel() {
					for _, prohibited := range prohibitedDimensionNames {
						if lp.GetName() == prohibited {
							t.Errorf("NFR-3 violation: metric %q has prohibited label %q",
								f.GetName(), prohibited)
						}
					}
				}
			}
		}
	}
}

// TestGolden_ByteStability asserts that given identical inputs, the metric
// output is byte-stable (deterministic).
func TestGolden_ByteStability(t *testing.T) {
	// We produce metrics from a known store state and verify that two
	// consecutive Gather() calls produce identical output.

	produce := func() string {
		reg := prometheus.NewRegistry()
		st := store.NewStore(10 * time.Minute)
		sm := NewSelfMetrics("1.0.0", "abc", "go1.21")
		client := cloudflare.NewClient("token", 5*time.Second)
		logger := newTestSetup(t).logger

		zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

		NewDNSCollector(client, st, sm, logger, 300, 60, 60, nil, zones, reg)

		// Seed the store with deterministic data
		ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		dims := store.MakeDimensionKey("zone_id", "z1", "zone_name", "example.com", "query_type", "A", "response_code", "NOERROR")
		st.Add("cloudflare_dns_queries_total", dims, ts, 100)

		families, err := reg.Gather()
		if err != nil {
			t.Fatalf("Gather failed: %v", err)
		}

		var sb strings.Builder
		enc := expfmt.NewEncoder(&sb, expfmt.NewFormat(expfmt.TypeTextPlain))
		for _, f := range families {
			enc.Encode(f)
		}
		return sb.String()
	}

	out1 := produce()
	out2 := produce()

	if out1 != out2 {
		t.Error("metric output is not byte-stable across identical inputs")
		t.Logf("Output 1:\n%s", out1)
		t.Logf("Output 2:\n%s", out2)
	}
}
