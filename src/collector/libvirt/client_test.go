package libvirt

import (
	"encoding/xml"
	"testing"
)

// testURI is the libvirt test driver URI which provides a fake hypervisor
// with one domain named "test" for use in unit tests.
const testURI = "test:///default"

// invalidURI is a URI that will always fail to connect.
const invalidURI = "test:///nonexistent"

// testDomainUUID returns the UUID of the "test" domain from the test driver.
func testDomainUUID(t *testing.T) string {
	t.Helper()
	c := &libvirtClient{uri: testURI}
	domains, err := c.ListDomains()
	if err != nil {
		t.Fatalf("listing domains: %v", err)
	}
	for _, d := range domains {
		if d.Name == "test" {
			return d.UUID
		}
	}
	t.Fatal("test domain not found in test:///default")
	return ""
}

// TestLibvirtClientConnectSuccess verifies that the real libvirtClient can
// connect to the libvirt test driver.
func TestLibvirtClientConnectSuccess(t *testing.T) {
	c := &libvirtClient{uri: testURI}
	conn, err := c.connect()
	if err != nil {
		t.Fatalf("expected successful connect, got error: %v", err)
	}
	conn.Close()
}

// TestLibvirtClientConnectError verifies that connect returns an error for
// an invalid URI.
func TestLibvirtClientConnectError(t *testing.T) {
	c := &libvirtClient{uri: invalidURI}
	conn, err := c.connect()
	if err == nil {
		conn.Close()
		t.Fatal("expected error from connect with invalid URI, got nil")
	}
}

// TestLibvirtClientIsAvailable verifies IsAvailable with both valid and
// invalid URIs.
func TestLibvirtClientIsAvailable(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		if !c.IsAvailable() {
			t.Error("expected IsAvailable to return true with test driver")
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		if c.IsAvailable() {
			t.Error("expected IsAvailable to return false with invalid URI")
		}
	})
}

// TestLibvirtClientGetNodeInfo verifies the getNodeInfo helper.
func TestLibvirtClientGetNodeInfo(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		info, err := c.getNodeInfo()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.Cpus == 0 {
			t.Error("expected non-zero CPUs")
		}
		if info.Memory == 0 {
			t.Error("expected non-zero Memory")
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.getNodeInfo()
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})
}

// TestLibvirtClientGetHostCPUCount verifies GetHostCPUCount with the test driver.
func TestLibvirtClientGetHostCPUCount(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		cpus, err := c.GetHostCPUCount()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cpus == 0 {
			t.Error("expected non-zero CPU count")
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.GetHostCPUCount()
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})
}

// TestLibvirtClientGetHostMemoryBytes verifies GetHostMemoryBytes with the test driver.
func TestLibvirtClientGetHostMemoryBytes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		mem, err := c.GetHostMemoryBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mem == 0 {
			t.Error("expected non-zero memory")
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.GetHostMemoryBytes()
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})
}

// TestLibvirtClientGetHostFreeMemoryBytes verifies GetHostFreeMemoryBytes with
// the test driver.
func TestLibvirtClientGetHostFreeMemoryBytes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		free, err := c.GetHostFreeMemoryBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if free == 0 {
			t.Error("expected non-zero free memory")
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.GetHostFreeMemoryBytes()
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})
}

// TestLibvirtClientListDomains verifies ListDomains with the test driver.
func TestLibvirtClientListDomains(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		domains, err := c.ListDomains()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(domains) == 0 {
			t.Fatal("expected at least one domain from test driver")
		}
		// The test driver provides a domain named "test".
		found := false
		for _, d := range domains {
			if d.Name == "test" {
				found = true
				if d.UUID == "" {
					t.Error("expected non-empty UUID")
				}
				if d.MaxMemory == 0 {
					t.Error("expected non-zero MaxMemory")
				}
			}
		}
		if !found {
			t.Error("expected domain named 'test' from test driver")
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.ListDomains()
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})
}

// TestLibvirtClientGetDomainMemoryStats verifies GetDomainMemoryStats with the
// test driver.
func TestLibvirtClientGetDomainMemoryStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uuid := testDomainUUID(t)
		c := &libvirtClient{uri: testURI}
		stats, err := c.GetDomainMemoryStats(uuid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(stats) == 0 {
			t.Error("expected at least one memory stat from test driver")
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.GetDomainMemoryStats("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})

	t.Run("domain_not_found", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		_, err := c.GetDomainMemoryStats("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent domain UUID")
		}
	})
}

// TestLibvirtClientGetDomainBlockStats verifies GetDomainBlockStats with the
// test driver.
func TestLibvirtClientGetDomainBlockStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uuid := testDomainUUID(t)
		c := &libvirtClient{uri: testURI}
		stats, err := c.GetDomainBlockStats(uuid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(stats) == 0 {
			t.Error("expected at least one block stat from test driver")
		}
		for _, s := range stats {
			if s.Device == "" {
				t.Error("expected non-empty device name")
			}
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.GetDomainBlockStats("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})

	t.Run("domain_not_found", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		_, err := c.GetDomainBlockStats("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent domain UUID")
		}
	})
}

// TestLibvirtClientGetDomainInterfaceStats verifies GetDomainInterfaceStats
// with the test driver.
func TestLibvirtClientGetDomainInterfaceStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uuid := testDomainUUID(t)
		c := &libvirtClient{uri: testURI}
		stats, err := c.GetDomainInterfaceStats(uuid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(stats) == 0 {
			t.Error("expected at least one interface stat from test driver")
		}
		for _, s := range stats {
			if s.Name == "" {
				t.Error("expected non-empty interface name")
			}
		}
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, err := c.GetDomainInterfaceStats("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})

	t.Run("domain_not_found", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		_, err := c.GetDomainInterfaceStats("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent domain UUID")
		}
	})
}

// TestParseDisks verifies the parseDisks helper.
func TestParseDisks(t *testing.T) {
	t.Run("valid_xml", func(t *testing.T) {
		xmlData := `<domain><devices>
			<disk><target dev='vda'/></disk>
			<disk><target dev='vdb'/></disk>
			<disk><target dev=''/></disk>
		</devices></domain>`

		devs, err := parseDisks(xmlData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devs) != 2 {
			t.Fatalf("expected 2 disks (empty dev filtered), got %d", len(devs))
		}
		if devs[0] != "vda" || devs[1] != "vdb" {
			t.Errorf("unexpected disk names: %v", devs)
		}
	})

	t.Run("no_disks", func(t *testing.T) {
		xmlData := `<domain><devices></devices></domain>`
		devs, err := parseDisks(xmlData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devs) != 0 {
			t.Errorf("expected 0 disks, got %d", len(devs))
		}
	})

	t.Run("invalid_xml", func(t *testing.T) {
		_, err := parseDisks("not valid xml <><>")
		if err == nil {
			t.Error("expected error for invalid XML")
		}
	})
}

// TestParseInterfaces verifies the parseInterfaces helper.
func TestParseInterfaces(t *testing.T) {
	t.Run("valid_xml", func(t *testing.T) {
		xmlData := `<domain><devices>
			<interface><target dev='vnet0'/></interface>
			<interface><target dev='vnet1'/></interface>
			<interface><target dev=''/></interface>
		</devices></domain>`

		devs, err := parseInterfaces(xmlData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devs) != 2 {
			t.Fatalf("expected 2 interfaces (empty dev filtered), got %d", len(devs))
		}
		if devs[0] != "vnet0" || devs[1] != "vnet1" {
			t.Errorf("unexpected interface names: %v", devs)
		}
	})

	t.Run("no_interfaces", func(t *testing.T) {
		xmlData := `<domain><devices></devices></domain>`
		devs, err := parseInterfaces(xmlData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(devs) != 0 {
			t.Errorf("expected 0 interfaces, got %d", len(devs))
		}
	})

	t.Run("invalid_xml", func(t *testing.T) {
		_, err := parseInterfaces("not valid xml <><>")
		if err == nil {
			t.Error("expected error for invalid XML")
		}
	})
}

// TestLibvirtClientLookupDomainByUUID verifies the lookupDomainByUUID helper.
func TestLibvirtClientLookupDomainByUUID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uuid := testDomainUUID(t)
		c := &libvirtClient{uri: testURI}
		conn, dom, err := c.lookupDomainByUUID(uuid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		dom.Free()
		conn.Close()
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, _, err := c.lookupDomainByUUID("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		_, _, err := c.lookupDomainByUUID("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent domain UUID")
		}
	})
}

// TestLibvirtClientGetDomainXMLDevices verifies the getDomainXML helper.
func TestLibvirtClientGetDomainXMLDevices(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uuid := testDomainUUID(t)
		c := &libvirtClient{uri: testURI}
		conn, dom, xmlDesc, err := c.getDomainXML(uuid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if xmlDesc == "" {
			t.Error("expected non-empty XML description")
		}
		dom.Free()
		conn.Close()
	})

	t.Run("connect_error", func(t *testing.T) {
		c := &libvirtClient{uri: invalidURI}
		_, _, _, err := c.getDomainXML("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error with invalid URI")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		c := &libvirtClient{uri: testURI}
		_, _, _, err := c.getDomainXML("00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent domain UUID")
		}
	})
}

// TestExtractDomainInfo verifies the extractDomainInfo helper with the test
// driver.
func TestExtractDomainInfo(t *testing.T) {
	c := &libvirtClient{uri: testURI}
	conn, err := c.connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName("test")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	defer dom.Free()

	d, err := extractDomainInfo(dom)
	if err != nil {
		t.Fatalf("extractDomainInfo: %v", err)
	}
	if d.Name != "test" {
		t.Errorf("expected name 'test', got %q", d.Name)
	}
	if d.UUID == "" {
		t.Error("expected non-empty UUID")
	}
	if d.MaxMemory == 0 {
		t.Error("expected non-zero MaxMemory")
	}
}

// TestXMLDomainParsing verifies the XML domain parsing structures work correctly.
func TestXMLDomainParsing(t *testing.T) {
	xmlData := `<domain>
  <devices>
    <disk type='file' device='disk'>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='disk'>
      <target dev='vdb' bus='virtio'/>
    </disk>
    <interface type='network'>
      <target dev='vnet0'/>
    </interface>
    <interface type='network'>
      <target dev='vnet1'/>
    </interface>
  </devices>
</domain>`

	var domXML xmlDomain
	if err := xml.Unmarshal([]byte(xmlData), &domXML); err != nil {
		t.Fatalf("failed to parse domain XML: %v", err)
	}

	if len(domXML.Devices.Disks) != 2 {
		t.Errorf("expected 2 disks, got %d", len(domXML.Devices.Disks))
	}
	if domXML.Devices.Disks[0].Target.Dev != "vda" {
		t.Errorf("expected first disk dev 'vda', got %q", domXML.Devices.Disks[0].Target.Dev)
	}

	if len(domXML.Devices.Interfaces) != 2 {
		t.Errorf("expected 2 interfaces, got %d", len(domXML.Devices.Interfaces))
	}
	if domXML.Devices.Interfaces[0].Target.Dev != "vnet0" {
		t.Errorf("expected first interface dev 'vnet0', got %q", domXML.Devices.Interfaces[0].Target.Dev)
	}
}
