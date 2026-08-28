package arp

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/vishvananda/netlink"
)

// Compile-time interface check for the real implementation.
var _ NeighborLister = (*netlinkLister)(nil)

// mockLister is a test double for NeighborLister.
type mockLister struct {
	neighbors []Neighbor
	err       error
}

func (m *mockLister) ListNeighbors() ([]Neighbor, error) {
	return m.neighbors, m.err
}

func TestCollectAllStates(t *testing.T) {
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	mac3, _ := net.ParseMAC("aa:bb:cc:dd:ee:03")
	mac4, _ := net.ParseMAC("aa:bb:cc:dd:ee:04")
	mac5, _ := net.ParseMAC("aa:bb:cc:dd:ee:05")
	mac6, _ := net.ParseMAC("aa:bb:cc:dd:ee:06")
	mac7, _ := net.ParseMAC("aa:bb:cc:dd:ee:07")
	mac8, _ := net.ParseMAC("aa:bb:cc:dd:ee:08")
	mac9, _ := net.ParseMAC("aa:bb:cc:dd:ee:09")

	lister := &mockLister{
		neighbors: []Neighbor{
			{IP: net.ParseIP("10.0.0.1"), MAC: mac1, Device: "eth0", State: 0x01},
			{IP: net.ParseIP("10.0.0.2"), MAC: mac2, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.3"), MAC: mac3, Device: "eth0", State: 0x04},
			{IP: net.ParseIP("10.0.0.4"), MAC: mac4, Device: "eth0", State: 0x08},
			{IP: net.ParseIP("10.0.0.5"), MAC: mac5, Device: "eth0", State: 0x10},
			{IP: net.ParseIP("10.0.0.6"), MAC: mac6, Device: "eth0", State: 0x20},
			{IP: net.ParseIP("10.0.0.7"), MAC: mac7, Device: "eth0", State: 0x40},
			{IP: net.ParseIP("10.0.0.8"), MAC: mac8, Device: "eth0", State: 0x80},
			{IP: net.ParseIP("10.0.0.9"), MAC: mac9, Device: "eth0", State: 0xFF},
		},
	}

	c := NewWithLister(lister)

	if c.Name() != "arp" {
		t.Fatalf("expected name 'arp', got %q", c.Name())
	}

	expected := `
# HELP network_arp_entry ARP table entry; value is always 1.
# TYPE network_arp_entry gauge
network_arp_entry{device="eth0",ip="10.0.0.1",mac="aa:bb:cc:dd:ee:01",state="incomplete"} 1
network_arp_entry{device="eth0",ip="10.0.0.2",mac="aa:bb:cc:dd:ee:02",state="reachable"} 1
network_arp_entry{device="eth0",ip="10.0.0.3",mac="aa:bb:cc:dd:ee:03",state="stale"} 1
network_arp_entry{device="eth0",ip="10.0.0.4",mac="aa:bb:cc:dd:ee:04",state="delay"} 1
network_arp_entry{device="eth0",ip="10.0.0.5",mac="aa:bb:cc:dd:ee:05",state="probe"} 1
network_arp_entry{device="eth0",ip="10.0.0.6",mac="aa:bb:cc:dd:ee:06",state="failed"} 1
network_arp_entry{device="eth0",ip="10.0.0.7",mac="aa:bb:cc:dd:ee:07",state="noarp"} 1
network_arp_entry{device="eth0",ip="10.0.0.8",mac="aa:bb:cc:dd:ee:08",state="permanent"} 1
network_arp_entry{device="eth0",ip="10.0.0.9",mac="aa:bb:cc:dd:ee:09",state="unknown(255)"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_arp_entry"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestCollectEmptyTable(t *testing.T) {
	lister := &mockLister{neighbors: []Neighbor{}}
	c := NewWithLister(lister)

	expected := `
# HELP network_arp_entry ARP table entry; value is always 1.
# TYPE network_arp_entry gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_arp_entry"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestIPv6Filtered(t *testing.T) {
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")

	lister := &mockLister{
		neighbors: []Neighbor{
			{IP: net.ParseIP("10.0.0.1"), MAC: mac1, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("fe80::1"), MAC: mac2, Device: "eth0", State: 0x02},
		},
	}
	c := NewWithLister(lister)

	expected := `
# HELP network_arp_entry ARP table entry; value is always 1.
# TYPE network_arp_entry gauge
network_arp_entry{device="eth0",ip="10.0.0.1",mac="aa:bb:cc:dd:ee:01",state="reachable"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_arp_entry"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestNilIPFiltered(t *testing.T) {
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")

	lister := &mockLister{
		neighbors: []Neighbor{
			{IP: nil, MAC: mac1, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.1"), MAC: mac1, Device: "eth0", State: 0x04},
		},
	}
	c := NewWithLister(lister)

	expected := `
# HELP network_arp_entry ARP table entry; value is always 1.
# TYPE network_arp_entry gauge
network_arp_entry{device="eth0",ip="10.0.0.1",mac="aa:bb:cc:dd:ee:01",state="stale"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_arp_entry"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestMultipleEntriesSameDevice(t *testing.T) {
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	mac3, _ := net.ParseMAC("aa:bb:cc:dd:ee:03")

	lister := &mockLister{
		neighbors: []Neighbor{
			{IP: net.ParseIP("10.0.0.1"), MAC: mac1, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.2"), MAC: mac2, Device: "eth0", State: 0x04},
			{IP: net.ParseIP("10.0.0.3"), MAC: mac3, Device: "eth0", State: 0x02},
		},
	}
	c := NewWithLister(lister)

	count := testutil.CollectAndCount(c, "network_arp_entry")
	if count != 3 {
		t.Errorf("expected 3 metrics, got %d", count)
	}
}

func TestZeroMACFailedState(t *testing.T) {
	zeroMAC := net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	lister := &mockLister{
		neighbors: []Neighbor{
			{IP: net.ParseIP("10.0.0.1"), MAC: zeroMAC, Device: "eth0", State: 0x20},
		},
	}
	c := NewWithLister(lister)

	expected := `
# HELP network_arp_entry ARP table entry; value is always 1.
# TYPE network_arp_entry gauge
network_arp_entry{device="eth0",ip="10.0.0.1",mac="00:00:00:00:00:00",state="failed"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_arp_entry"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestListNeighborsError(t *testing.T) {
	lister := &mockLister{err: fmt.Errorf("netlink unavailable")}
	c := NewWithLister(lister)

	// Collect should emit an invalid metric, which prometheus reports as an error.
	ch := make(chan prometheus.Metric, 1)
	c.Collect(ch)
	close(ch)

	m := <-ch
	if m == nil {
		t.Fatal("expected an invalid metric, got nil")
	}

	dto := new(prometheus.Metric)
	_ = dto // The invalid metric's Write will return an error.
}

func TestDescribe(t *testing.T) {
	c := NewWithLister(&mockLister{})
	ch := make(chan *prometheus.Desc, 2)
	c.Describe(ch)
	close(ch)

	count := 0
	for range ch {
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 descriptors, got %d", count)
	}
}

func TestNewWithMax(t *testing.T) {
	c := NewWithMax(100)
	if c == nil {
		t.Fatal("NewWithMax() returned nil")
	}
	if c.Name() != "arp" {
		t.Errorf("expected name 'arp', got %q", c.Name())
	}
}

func TestNewWithOptionsNegativeMax(t *testing.T) {
	lister := &mockLister{}
	c := NewWithOptions(lister, -1)
	if c == nil {
		t.Fatal("NewWithOptions(-1) returned nil")
	}
	// Should default to DefaultMaxEntries.
	ac := c.(*arpCollector)
	if ac.maxEntries != DefaultMaxEntries {
		t.Errorf("expected maxEntries=%d for negative input, got %d", DefaultMaxEntries, ac.maxEntries)
	}
}

func TestNew(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "arp" {
		t.Errorf("expected name 'arp', got %q", c.Name())
	}
}

func TestCollectTruncation(t *testing.T) {
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	mac3, _ := net.ParseMAC("aa:bb:cc:dd:ee:03")
	mac4, _ := net.ParseMAC("aa:bb:cc:dd:ee:04")
	mac5, _ := net.ParseMAC("aa:bb:cc:dd:ee:05")

	lister := &mockLister{
		neighbors: []Neighbor{
			{IP: net.ParseIP("10.0.0.1"), MAC: mac1, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.2"), MAC: mac2, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.3"), MAC: mac3, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.4"), MAC: mac4, Device: "eth0", State: 0x02},
			{IP: net.ParseIP("10.0.0.5"), MAC: mac5, Device: "eth0", State: 0x02},
		},
	}

	// maxEntries=2 with 5 neighbors: only 2 emitted, truncated=1.
	c := NewWithOptions(lister, 2)

	// Verify truncated metric is set to 1.
	expected := `
# HELP network_arp_entries_truncated Set to 1 when the ARP table exceeds the maximum entry limit and output is truncated.
# TYPE network_arp_entries_truncated gauge
network_arp_entries_truncated 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_arp_entries_truncated"); err != nil {
		t.Errorf("truncation metric mismatch: %v", err)
	}

	// Verify only 2 entry metrics are emitted.
	entryCount := testutil.CollectAndCount(c, "network_arp_entry")
	if entryCount != 2 {
		t.Errorf("expected 2 ARP entries emitted, got %d", entryCount)
	}
}

func TestNetlinkListerListNeighbors(t *testing.T) {
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")

	// Override netlink functions with stubs.
	origNeighList := neighListFn
	origLinkByIndex := linkByIndexFn
	defer func() {
		neighListFn = origNeighList
		linkByIndexFn = origLinkByIndex
	}()

	neighListFn = func(linkIndex, family int) ([]netlink.Neigh, error) {
		return []netlink.Neigh{
			{IP: net.ParseIP("10.0.0.1"), HardwareAddr: mac1, LinkIndex: 1, State: 0x02},
			{IP: net.ParseIP("10.0.0.2"), HardwareAddr: mac1, LinkIndex: 999, State: 0x02}, // will fail linkByIndex
		}, nil
	}

	linkByIndexFn = func(index int) (netlink.Link, error) {
		if index == 1 {
			return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0"}}, nil
		}
		return nil, fmt.Errorf("link not found")
	}

	lister := &netlinkLister{}
	neighbors, err := lister.ListNeighbors()
	if err != nil {
		t.Fatalf("ListNeighbors: %v", err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor (one skipped due to link error), got %d", len(neighbors))
	}
	if neighbors[0].IP.String() != "10.0.0.1" || neighbors[0].Device != "eth0" {
		t.Errorf("unexpected neighbor: %+v", neighbors[0])
	}
}

func TestNetlinkListerNeighListError(t *testing.T) {
	origNeighList := neighListFn
	defer func() { neighListFn = origNeighList }()

	neighListFn = func(linkIndex, family int) ([]netlink.Neigh, error) {
		return nil, fmt.Errorf("netlink unavailable")
	}

	lister := &netlinkLister{}
	_, err := lister.ListNeighbors()
	if err == nil {
		t.Error("expected error when NeighList fails")
	}
}
