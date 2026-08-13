package iface

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Compile-time interface check for the real implementation.
var _ LinkLister = (*sysfsLister)(nil)

// mockLister is a test double for LinkLister.
type mockLister struct {
	links []LinkInfo
	err   error
}

func (m *mockLister) ListLinks() ([]LinkInfo, error) {
	return m.links, m.err
}

func TestCollectFullTopology(t *testing.T) {
	lister := &mockLister{
		links: []LinkInfo{
			{Name: "lo", Type: "loopback", Driver: ""},
			{Name: "eth0", Type: "physical", Driver: "e1000e"},
			{Name: "eth1", Type: "physical", Driver: "e1000e", MasterName: "bond0", MasterType: "bond"},
			{Name: "eth2", Type: "physical", Driver: "e1000e", MasterName: "bond0", MasterType: "bond"},
			{Name: "bond0", Type: "bond", Driver: "bonding"},
			{Name: "br0", Type: "bridge", Driver: "bridge"},
			{Name: "veth0", Type: "veth", Driver: "", MasterName: "br0", MasterType: "bridge"},
			{Name: "veth1", Type: "veth", Driver: "", MasterName: "br0", MasterType: "bridge"},
			{Name: "vti0", Type: "vti", Driver: ""},
		},
	}

	c := NewWithLister(lister)

	if c.Name() != "iface" {
		t.Fatalf("expected name 'iface', got %q", c.Name())
	}

	expected := `
# HELP network_interface_type Interface type classification; value is always 1.
# TYPE network_interface_type gauge
network_interface_type{device="lo",driver="",type="loopback"} 1
network_interface_type{device="eth0",driver="e1000e",type="physical"} 1
network_interface_type{device="eth1",driver="e1000e",type="physical"} 1
network_interface_type{device="eth2",driver="e1000e",type="physical"} 1
network_interface_type{device="bond0",driver="bonding",type="bond"} 1
network_interface_type{device="br0",driver="bridge",type="bridge"} 1
network_interface_type{device="veth0",driver="",type="veth"} 1
network_interface_type{device="veth1",driver="",type="veth"} 1
network_interface_type{device="vti0",driver="",type="vti"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_interface_type"); err != nil {
		t.Errorf("type metric mismatch: %v", err)
	}

	expectedBond := `
# HELP network_bond_member Bond membership; value is always 1.
# TYPE network_bond_member gauge
network_bond_member{bond="bond0",member="eth1"} 1
network_bond_member{bond="bond0",member="eth2"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expectedBond), "network_bond_member"); err != nil {
		t.Errorf("bond metric mismatch: %v", err)
	}

	expectedBridge := `
# HELP network_bridge_member Bridge membership; value is always 1.
# TYPE network_bridge_member gauge
network_bridge_member{bridge="br0",member="veth0"} 1
network_bridge_member{bridge="br0",member="veth1"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expectedBridge), "network_bridge_member"); err != nil {
		t.Errorf("bridge metric mismatch: %v", err)
	}
}

func TestCollectEmptyList(t *testing.T) {
	lister := &mockLister{links: []LinkInfo{}}
	c := NewWithLister(lister)

	expected := `
# HELP network_interface_type Interface type classification; value is always 1.
# TYPE network_interface_type gauge
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_interface_type"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestInterfaceNoDriver(t *testing.T) {
	lister := &mockLister{
		links: []LinkInfo{
			{Name: "tun0", Type: "other", Driver: ""},
		},
	}
	c := NewWithLister(lister)

	expected := `
# HELP network_interface_type Interface type classification; value is always 1.
# TYPE network_interface_type gauge
network_interface_type{device="tun0",driver="",type="other"} 1
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "network_interface_type"); err != nil {
		t.Errorf("metric mismatch: %v", err)
	}
}

func TestAllTypeClassifications(t *testing.T) {
	lister := &mockLister{
		links: []LinkInfo{
			{Name: "eth0", Type: "physical", Driver: "igb"},
			{Name: "bond0", Type: "bond", Driver: "bonding"},
			{Name: "br0", Type: "bridge", Driver: "bridge"},
			{Name: "vti0", Type: "vti", Driver: ""},
			{Name: "veth0", Type: "veth", Driver: ""},
			{Name: "lo", Type: "loopback", Driver: ""},
			{Name: "gre0", Type: "other", Driver: ""},
		},
	}
	c := NewWithLister(lister)

	count := testutil.CollectAndCount(c, "network_interface_type")
	if count != 7 {
		t.Errorf("expected 7 type metrics, got %d", count)
	}
}

func TestBondMembershipOnly(t *testing.T) {
	lister := &mockLister{
		links: []LinkInfo{
			{Name: "eth0", Type: "physical", Driver: "e1000e", MasterName: "bond0", MasterType: "bond"},
			{Name: "bond0", Type: "bond", Driver: "bonding"},
		},
	}
	c := NewWithLister(lister)

	bondCount := testutil.CollectAndCount(c, "network_bond_member")
	if bondCount != 1 {
		t.Errorf("expected 1 bond member metric, got %d", bondCount)
	}
	bridgeCount := testutil.CollectAndCount(c, "network_bridge_member")
	if bridgeCount != 0 {
		t.Errorf("expected 0 bridge member metrics, got %d", bridgeCount)
	}
}

func TestBridgeMembershipOnly(t *testing.T) {
	lister := &mockLister{
		links: []LinkInfo{
			{Name: "veth0", Type: "veth", Driver: "", MasterName: "br0", MasterType: "bridge"},
			{Name: "br0", Type: "bridge", Driver: "bridge"},
		},
	}
	c := NewWithLister(lister)

	bridgeCount := testutil.CollectAndCount(c, "network_bridge_member")
	if bridgeCount != 1 {
		t.Errorf("expected 1 bridge member metric, got %d", bridgeCount)
	}
	bondCount := testutil.CollectAndCount(c, "network_bond_member")
	if bondCount != 0 {
		t.Errorf("expected 0 bond member metrics, got %d", bondCount)
	}
}

func TestListLinksError(t *testing.T) {
	lister := &mockLister{err: fmt.Errorf("sysfs unavailable")}
	c := NewWithLister(lister)

	ch := make(chan prometheus.Metric, 4)
	c.Collect(ch)
	close(ch)

	m := <-ch
	if m == nil {
		t.Fatal("expected an invalid metric, got nil")
	}
}

func TestDescribe(t *testing.T) {
	c := NewWithLister(&mockLister{})
	ch := make(chan *prometheus.Desc, 3)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 3 {
		t.Fatalf("expected 3 descriptors, got %d", len(descs))
	}
}

func TestNew(t *testing.T) {
	c := New("/sys")
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.Name() != "iface" {
		t.Errorf("expected name 'iface', got %q", c.Name())
	}
}

func TestSysfsListerListLinks(t *testing.T) {
	// Build a fake sysfs tree.
	root := t.TempDir()
	netDir := filepath.Join(root, "class", "net")

	// eth0 — physical (type=0, no bonding/bridge dir)
	mkIface(t, netDir, "eth0", "0", "", "")

	// bond0 — has bonding dir
	mkIface(t, netDir, "bond0", "0", "bonding", "")

	// br0 — has bridge dir
	mkIface(t, netDir, "br0", "0", "", "bridge")

	// lo — loopback (type=772)
	mkIface(t, netDir, "lo", "772", "", "")

	// veth0 — veth (type=0, name starts with veth)
	mkIface(t, netDir, "veth0", "0", "", "")

	// vti0 — tunnel (type=768)
	mkIface(t, netDir, "vti0", "768", "", "")

	// tun0 — other (type=65534)
	mkIface(t, netDir, "tun0", "65534", "", "")

	// eth1 — slave of bond0
	mkIfaceWithMaster(t, netDir, "eth1", "0", "bond0")

	// veth1 — slave of br0
	mkIfaceWithMaster(t, netDir, "veth1", "0", "br0")

	lister := &sysfsLister{sysPath: root}
	links, err := lister.ListLinks()
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}

	byName := make(map[string]LinkInfo)
	for _, li := range links {
		byName[li.Name] = li
	}

	assertType := func(name, expectedType string) {
		t.Helper()
		li, ok := byName[name]
		if !ok {
			t.Errorf("missing interface %s", name)
			return
		}
		if li.Type != expectedType {
			t.Errorf("%s: expected type %q, got %q", name, expectedType, li.Type)
		}
	}

	assertType("eth0", "physical")
	assertType("bond0", "bond")
	assertType("br0", "bridge")
	assertType("lo", "loopback")
	assertType("veth0", "veth")
	assertType("vti0", "vti")
	assertType("tun0", "other")

	// Check master relationships.
	if eth1 := byName["eth1"]; eth1.MasterName != "bond0" || eth1.MasterType != "bond" {
		t.Errorf("eth1: expected master bond0/bond, got %s/%s", eth1.MasterName, eth1.MasterType)
	}
	if veth1 := byName["veth1"]; veth1.MasterName != "br0" || veth1.MasterType != "bridge" {
		t.Errorf("veth1: expected master br0/bridge, got %s/%s", veth1.MasterName, veth1.MasterType)
	}
}

func TestSysfsListerDriverSymlink(t *testing.T) {
	root := t.TempDir()
	netDir := filepath.Join(root, "class", "net")
	devDir := filepath.Join(netDir, "eth0")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "type"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create device/driver symlink.
	deviceDir := filepath.Join(devDir, "device")
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Point driver to a fake path with the driver name as the base.
	if err := os.Symlink("/fake/drivers/e1000e", filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	lister := &sysfsLister{sysPath: root}
	links, err := lister.ListLinks()
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Driver != "e1000e" {
		t.Errorf("expected driver 'e1000e', got %q", links[0].Driver)
	}
}

func TestSysfsListerMasterNotInMap(t *testing.T) {
	// Interface with a master symlink pointing to an interface not in the map
	// (e.g. the master dir doesn't exist in netDir).
	root := t.TempDir()
	netDir := filepath.Join(root, "class", "net")
	mkIface(t, netDir, "eth0", "0", "", "")
	devDir := filepath.Join(netDir, "eth0")
	// Create master symlink to a non-existent interface.
	if err := os.Symlink(filepath.Join(netDir, "nonexistent_master"), filepath.Join(devDir, "master")); err != nil {
		t.Fatal(err)
	}

	lister := &sysfsLister{sysPath: root}
	links, err := lister.ListLinks()
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].MasterName != "nonexistent_master" {
		t.Errorf("expected master name 'nonexistent_master', got %q", links[0].MasterName)
	}
	// Master type should be empty since the master is not in the map.
	if links[0].MasterType != "" {
		t.Errorf("expected empty master type, got %q", links[0].MasterType)
	}
}

func TestSysfsListerBadPath(t *testing.T) {
	lister := &sysfsLister{sysPath: "/nonexistent"}
	_, err := lister.ListLinks()
	if err == nil {
		t.Error("expected error for nonexistent sysfs path")
	}
}

func TestClassifyByTypeFileMissingTypeFile(t *testing.T) {
	root := t.TempDir()
	netDir := filepath.Join(root, "class", "net")
	devDir := filepath.Join(netDir, "nofile")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No type file written — classifyByTypeFile should return "other".
	lister := &sysfsLister{sysPath: root}
	result := lister.classifyByTypeFile(devDir, "nofile")
	if result != "other" {
		t.Errorf("expected 'other', got %q", result)
	}
}

// mkIface creates a fake sysfs interface directory.
func mkIface(t *testing.T, netDir, name, typeVal, bondDir, bridgeDir string) {
	t.Helper()
	devDir := filepath.Join(netDir, name)
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "type"), []byte(typeVal+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bondDir != "" {
		if err := os.MkdirAll(filepath.Join(devDir, bondDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if bridgeDir != "" {
		if err := os.MkdirAll(filepath.Join(devDir, bridgeDir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// mkIfaceWithMaster creates a fake sysfs interface with a master symlink.
func mkIfaceWithMaster(t *testing.T, netDir, name, typeVal, masterName string) {
	t.Helper()
	mkIface(t, netDir, name, typeVal, "", "")
	devDir := filepath.Join(netDir, name)
	// Create a symlink "master" pointing to the master interface directory.
	if err := os.Symlink(filepath.Join(netDir, masterName), filepath.Join(devDir, "master")); err != nil {
		t.Fatal(err)
	}
}

func TestNoMembershipWhenMasterTypeEmpty(t *testing.T) {
	// An interface with a master but unknown master type should not
	// generate bond or bridge membership metrics.
	lister := &mockLister{
		links: []LinkInfo{
			{Name: "eth0", Type: "physical", Driver: "e1000e", MasterName: "unknown0", MasterType: ""},
		},
	}
	c := NewWithLister(lister)

	bondCount := testutil.CollectAndCount(c, "network_bond_member")
	if bondCount != 0 {
		t.Errorf("expected 0 bond member metrics, got %d", bondCount)
	}
	bridgeCount := testutil.CollectAndCount(c, "network_bridge_member")
	if bridgeCount != 0 {
		t.Errorf("expected 0 bridge member metrics, got %d", bridgeCount)
	}
}
