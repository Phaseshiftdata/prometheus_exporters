package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/governor"
)

// TestAllCollectors_InterfaceMethods exercises Priority(), Interval(),
// RequiredDatasets() for every collector to cover single-line getters.
func TestAllCollectors_InterfaceMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeGraphQLResponse(map[string]interface{}{
			"viewer": map[string]interface{}{"accounts": []interface{}{}, "zones": []interface{}{}},
		}))
	}))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	tests := []struct {
		name             string
		priority         governor.PriorityClass
		interval         time.Duration
		requiredDatasets int
		newFunc          func() Collector
	}{
		{
			name:             "access",
			priority:         governor.PriorityCritical,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
				return c
			},
		},
		{
			name:             "gateway_dns",
			priority:         governor.PriorityCritical,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
				return c
			},
		},
		{
			name:             "gateway_network",
			priority:         governor.PriorityCritical,
			interval:         60 * time.Second,
			requiredDatasets: 3,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
				return c
			},
		},
		{
			name:             "browser_isolation",
			priority:         governor.PriorityStandard,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
				return c
			},
		},
		{
			name:             "tunnel",
			priority:         governor.PriorityStandard,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
				return c
			},
		},
		{
			name:             "dns",
			priority:         governor.PriorityStandard,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, ts.registry)
				return c
			},
		},
		{
			name:             "dns_firewall",
			priority:         governor.PriorityStandard,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, nil, ts.registry)
				return c
			},
		},
		{
			name:             "domain",
			priority:         governor.PriorityBackground,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, nil, ts.registry)
				return c
			},
		},
		{
			name:             "certificate",
			priority:         governor.PriorityBackground,
			interval:         60 * time.Second,
			requiredDatasets: 1,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, ts.registry)
				return c
			},
		},
		{
			name:             "zone_status",
			priority:         governor.PriorityBackground,
			interval:         60 * time.Second,
			requiredDatasets: 0,
			newFunc: func() Collector {
				ts := newTestSetup(t)
				c, _ := NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, nil, ts.registry)
				return c
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.newFunc()
			if c.Name() != tt.name {
				t.Fatalf("Name() = %q, want %q", c.Name(), tt.name)
			}
			if c.Priority() != tt.priority {
				t.Fatalf("Priority() = %d, want %d", c.Priority(), tt.priority)
			}
			if c.Interval() != tt.interval {
				t.Fatalf("Interval() = %v, want %v", c.Interval(), tt.interval)
			}
			if len(c.RequiredDatasets()) != tt.requiredDatasets {
				t.Fatalf("RequiredDatasets() has %d items, want %d", len(c.RequiredDatasets()), tt.requiredDatasets)
			}
			if c.Describe() == "" {
				t.Fatal("Describe() returned empty string")
			}
		})
	}
}

// TestNewCollector_RegistrationError verifies that registering a collector with
// a registry that already has the same descriptor returns an error.
func TestNewCollector_RegistrationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	client := createTestClient(server)
	zones := []ZoneInfo{{ID: "z1", Name: "example.com"}}

	tests := []struct {
		name string
		fn   func(ts testSetup) error
	}{
		{"access", func(ts testSetup) error {
			_, err := NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			if err != nil {
				return err
			}
			_, err = NewAccessCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			return err
		}},
		{"gateway_dns", func(ts testSetup) error {
			NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			_, err := NewGatewayDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			return err
		}},
		{"gateway_network", func(ts testSetup) error {
			NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			_, err := NewGatewayNetworkCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			return err
		}},
		{"browser_isolation", func(ts testSetup) error {
			NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			_, err := NewBrowserIsolationCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			return err
		}},
		{"tunnel", func(ts testSetup) error {
			NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			_, err := NewTunnelCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, []string{"a"}, ts.registry)
			return err
		}},
		{"dns", func(ts testSetup) error {
			NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, ts.registry)
			_, err := NewDNSCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, ts.registry)
			return err
		}},
		{"dns_firewall", func(ts testSetup) error {
			NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, nil, ts.registry)
			_, err := NewDNSFirewallCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, nil, ts.registry)
			return err
		}},
		{"domain", func(ts testSetup) error {
			NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, nil, ts.registry)
			_, err := NewDomainCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, nil, ts.registry)
			return err
		}},
		{"certificate", func(ts testSetup) error {
			NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, ts.registry)
			_, err := NewCertificateCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, ts.registry)
			return err
		}},
		{"zone_status", func(ts testSetup) error {
			NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, nil, ts.registry)
			_, err := NewZoneStatusCollector(client, ts.store, ts.selfMetrics, ts.logger, 300, 60, 60, nil, zones, nil, ts.registry)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestSetup(t)
			err := tt.fn(ts)
			if err == nil {
				t.Fatal("expected error on duplicate registration")
			}
		})
	}
}
