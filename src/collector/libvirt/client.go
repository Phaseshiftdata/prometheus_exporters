package libvirt

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/url"

	lv "libvirt.org/go/libvirt"
)

// redactURI strips any userinfo (username:password) from a URI before
// it appears in error messages or logs.
func redactURI(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-uri>"
	}
	u.User = nil
	return u.String()
}

// Compile-time interface check.
var _ LibvirtClient = (*libvirtClient)(nil)

// xmlDomain is used to parse domain XML for device discovery.
type xmlDomain struct {
	Devices xmlDevices `xml:"devices"`
}

type xmlDevices struct {
	Disks      []xmlDisk      `xml:"disk"`
	Interfaces []xmlInterface `xml:"interface"`
}

type xmlDisk struct {
	Target xmlDiskTarget `xml:"target"`
}

type xmlDiskTarget struct {
	Dev string `xml:"dev,attr"`
}

type xmlInterface struct {
	Target xmlInterfaceTarget `xml:"target"`
}

type xmlInterfaceTarget struct {
	Dev string `xml:"dev,attr"`
}

// parseDisks extracts non-empty disk device names from domain XML.
func parseDisks(xmlData string) ([]string, error) {
	var domXML xmlDomain
	if err := xml.Unmarshal([]byte(xmlData), &domXML); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}

	var devs []string
	for _, disk := range domXML.Devices.Disks {
		if disk.Target.Dev != "" {
			devs = append(devs, disk.Target.Dev)
		}
	}
	return devs, nil
}

// parseInterfaces extracts non-empty interface device names from domain XML.
func parseInterfaces(xmlData string) ([]string, error) {
	var domXML xmlDomain
	if err := xml.Unmarshal([]byte(xmlData), &domXML); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}

	var devs []string
	for _, iface := range domXML.Devices.Interfaces {
		if iface.Target.Dev != "" {
			devs = append(devs, iface.Target.Dev)
		}
	}
	return devs, nil
}

// libvirtClient implements LibvirtClient by connecting to libvirtd.
type libvirtClient struct {
	uri string
}

func (c *libvirtClient) connect() (*lv.Connect, error) {
	conn, err := lv.NewConnect(c.uri)
	if err != nil {
		return nil, fmt.Errorf("libvirt connect %s: %w", redactURI(c.uri), err)
	}
	return conn, nil
}

func (c *libvirtClient) IsAvailable() bool {
	conn, err := c.connect()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// getNodeInfo connects to libvirtd and retrieves node information.
func (c *libvirtClient) getNodeInfo() (*lv.NodeInfo, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	info, err := conn.GetNodeInfo()
	if err != nil {
		return nil, fmt.Errorf("get node info: %w", err)
	}
	return info, nil
}

func (c *libvirtClient) GetHostCPUCount() (uint, error) {
	info, err := c.getNodeInfo()
	if err != nil {
		return 0, err
	}
	return info.Cpus, nil
}

func (c *libvirtClient) GetHostMemoryBytes() (uint64, error) {
	info, err := c.getNodeInfo()
	if err != nil {
		return 0, err
	}
	// NodeInfo.Memory is in KiB.
	return info.Memory * 1024, nil
}

func (c *libvirtClient) GetHostFreeMemoryBytes() (uint64, error) {
	conn, err := c.connect()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	free, err := conn.GetFreeMemory()
	if err != nil {
		return 0, fmt.Errorf("get free memory: %w", err)
	}
	return free, nil
}

func (c *libvirtClient) ListDomains() ([]DomainInfo, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	domains, err := conn.ListAllDomains(lv.CONNECT_LIST_DOMAINS_ACTIVE | lv.CONNECT_LIST_DOMAINS_INACTIVE)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}

	var result []DomainInfo
	for _, dom := range domains {
		d, err := extractDomainInfo(&dom)
		dom.Free()
		if err != nil {
			continue
		}
		result = append(result, d)
	}

	return result, nil
}

// extractDomainInfo reads name, UUID, and resource info from a libvirt domain.
func extractDomainInfo(dom *lv.Domain) (DomainInfo, error) {
	name, err := dom.GetName()
	if err != nil {
		return DomainInfo{}, err
	}
	uuid, err := dom.GetUUIDString()
	if err != nil {
		return DomainInfo{}, err
	}
	info, err := dom.GetInfo()
	if err != nil {
		return DomainInfo{}, err
	}
	return DomainInfo{
		Name:      name,
		UUID:      uuid,
		State:     uint8(info.State),
		MaxMemory: info.MaxMem * 1024, // KiB to bytes
		Memory:    info.Memory * 1024,  // KiB to bytes
		NrVirtCPU: info.NrVirtCpu,
		CPUTime:   info.CpuTime,
	}, nil
}

// lookupDomainByUUID connects and looks up a domain by UUID. Using UUID
// instead of name eliminates TOCTOU races where a domain is destroyed and
// recreated with the same name between ListDomains and stats collection.
// The caller must call conn.Close() and dom.Free() when done.
func (c *libvirtClient) lookupDomainByUUID(uuid string) (*lv.Connect, *lv.Domain, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, nil, err
	}
	dom, err := conn.LookupDomainByUUIDString(uuid)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("lookup domain uuid %s: %w", uuid, err)
	}
	return conn, dom, nil
}

// getDomainXML calls lookupDomainByUUID and then retrieves the domain XML.
// On any failure after lookup, it closes the connection and frees the
// domain before returning the error.
func (c *libvirtClient) getDomainXML(uuid string) (*lv.Connect, *lv.Domain, string, error) {
	conn, dom, err := c.lookupDomainByUUID(uuid)
	if err != nil {
		return nil, nil, "", err
	}
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		dom.Free()
		conn.Close()
		return nil, nil, "", fmt.Errorf("get xml desc uuid %s: %w", uuid, err)
	}
	return conn, dom, xmlDesc, nil
}

func (c *libvirtClient) GetDomainMemoryStats(uuid string) ([]DomainMemoryStat, error) {
	conn, dom, err := c.lookupDomainByUUID(uuid)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer dom.Free()

	stats, err := dom.MemoryStats(uint32(lv.DOMAIN_MEMORY_STAT_NR), 0)
	if err != nil {
		return nil, fmt.Errorf("memory stats uuid %s: %w", uuid, err)
	}

	var result []DomainMemoryStat
	for _, s := range stats {
		result = append(result, DomainMemoryStat{
			Tag: int32(s.Tag),
			Val: s.Val,
		})
	}
	return result, nil
}

func (c *libvirtClient) GetDomainBlockStats(uuid string) ([]DomainBlockStats, error) {
	conn, dom, xmlDesc, err := c.getDomainXML(uuid)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer dom.Free()

	devs, err := parseDisks(xmlDesc)
	if err != nil {
		return nil, fmt.Errorf("parse disks uuid %s: %w", uuid, err)
	}

	var result []DomainBlockStats
	for _, dev := range devs {
		bs, err := dom.BlockStats(dev)
		if err != nil {
			slog.Debug("failed to get block stats for device", "uuid", uuid, "device", dev, "error", err)
			continue
		}
		result = append(result, DomainBlockStats{
			Device:  dev,
			RdBytes: bs.RdBytes,
			WrBytes: bs.WrBytes,
			RdReq:   bs.RdReq,
			WrReq:   bs.WrReq,
		})
	}

	return result, nil
}

func (c *libvirtClient) GetDomainInterfaceStats(uuid string) ([]DomainInterfaceStats, error) {
	conn, dom, xmlDesc, err := c.getDomainXML(uuid)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer dom.Free()

	devs, err := parseInterfaces(xmlDesc)
	if err != nil {
		return nil, fmt.Errorf("parse interfaces uuid %s: %w", uuid, err)
	}

	var result []DomainInterfaceStats
	for _, dev := range devs {
		is, err := dom.InterfaceStats(dev)
		if err != nil {
			slog.Debug("failed to get interface stats for device", "uuid", uuid, "interface", dev, "error", err)
			continue
		}
		result = append(result, DomainInterfaceStats{
			Name:      dev,
			RxBytes:   is.RxBytes,
			TxBytes:   is.TxBytes,
			RxPackets: is.RxPackets,
			TxPackets: is.TxPackets,
			RxErrs:    is.RxErrs,
			TxErrs:    is.TxErrs,
		})
	}

	return result, nil
}
