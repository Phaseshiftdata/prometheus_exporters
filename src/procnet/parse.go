package procnet

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseTCP reads <procPath>/net/tcp and <procPath>/net/tcp6, parses all entries,
// and returns them combined. Each entry has Protocol="tcp".
func ParseTCP(procPath string) ([]Entry, error) {
	var all []Entry

	tcp4, err4 := parseFile(filepath.Join(procPath, "net", "tcp"), "tcp")
	if err4 != nil && !errors.Is(err4, fs.ErrNotExist) {
		return nil, fmt.Errorf("parsing tcp: %w", err4)
	}
	all = append(all, tcp4...)

	tcp6, err6 := parseFile(filepath.Join(procPath, "net", "tcp6"), "tcp")
	if err6 != nil && !errors.Is(err6, fs.ErrNotExist) {
		return nil, fmt.Errorf("parsing tcp6: %w", err6)
	}
	all = append(all, tcp6...)

	if errors.Is(err4, fs.ErrNotExist) && errors.Is(err6, fs.ErrNotExist) {
		return nil, fmt.Errorf("neither tcp nor tcp6 found under %s/net", procPath)
	}

	return all, nil
}

// ParseUDP reads <procPath>/net/udp and <procPath>/net/udp6, parses all entries,
// and returns them combined. Each entry has Protocol="udp".
func ParseUDP(procPath string) ([]Entry, error) {
	var all []Entry

	udp4, err4 := parseFile(filepath.Join(procPath, "net", "udp"), "udp")
	if err4 != nil && !errors.Is(err4, fs.ErrNotExist) {
		return nil, fmt.Errorf("parsing udp: %w", err4)
	}
	all = append(all, udp4...)

	udp6, err6 := parseFile(filepath.Join(procPath, "net", "udp6"), "udp")
	if err6 != nil && !errors.Is(err6, fs.ErrNotExist) {
		return nil, fmt.Errorf("parsing udp6: %w", err6)
	}
	all = append(all, udp6...)

	if errors.Is(err4, fs.ErrNotExist) && errors.Is(err6, fs.ErrNotExist) {
		return nil, fmt.Errorf("neither udp nor udp6 found under %s/net", procPath)
	}

	return all, nil
}

// parseFile parses a single /proc/net/{tcp,tcp6,udp,udp6} file.
func parseFile(path, protocol string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)

	// Skip header line.
	if !scanner.Scan() {
		return entries, scanner.Err()
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		entry, err := parseLine(line, protocol)
		if err != nil {
			// Skip malformed lines gracefully.
			continue
		}
		entries = append(entries, entry)
	}

	return entries, scanner.Err()
}

// parseLine parses a single line from a /proc/net file.
// Format:
//
//	sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode
//	 0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345
func parseLine(line, protocol string) (Entry, error) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return Entry{}, fmt.Errorf("not enough fields: %d", len(fields))
	}

	// fields[0] = "0:" (sl, slot number — ignore)
	// fields[1] = local_address (hex_ip:hex_port)
	// fields[2] = rem_address (hex_ip:hex_port)
	// fields[3] = st (hex state)
	// fields[4] = tx_queue:rx_queue
	// fields[5] = tr:tm->when
	// fields[6] = retrnsmt
	// fields[7] = uid
	// fields[8] = timeout
	// fields[9] = inode

	localIP, localPort, err := parseAddress(fields[1])
	if err != nil {
		return Entry{}, fmt.Errorf("parsing local address: %w", err)
	}

	remoteIP, remotePort, err := parseAddress(fields[2])
	if err != nil {
		return Entry{}, fmt.Errorf("parsing remote address: %w", err)
	}

	state := stateName(strings.ToUpper(fields[3]))

	txRx := strings.SplitN(fields[4], ":", 2)
	if len(txRx) != 2 {
		return Entry{}, fmt.Errorf("invalid tx_queue:rx_queue: %s", fields[4])
	}
	txQueue, err := strconv.ParseUint(txRx[0], 16, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("parsing tx_queue: %w", err)
	}
	rxQueue, err := strconv.ParseUint(txRx[1], 16, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("parsing rx_queue: %w", err)
	}

	uid, err := strconv.ParseUint(fields[7], 10, 32)
	if err != nil {
		return Entry{}, fmt.Errorf("parsing uid: %w", err)
	}

	inode, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("parsing inode: %w", err)
	}

	return Entry{
		LocalIP:    localIP,
		LocalPort:  localPort,
		RemoteIP:   remoteIP,
		RemotePort: remotePort,
		State:      state,
		Protocol:   protocol,
		TxQueue:    txQueue,
		RxQueue:    rxQueue,
		UID:        uint32(uid),
		Inode:      inode,
	}, nil
}

// parseAddress parses a hex address string like "0100007F:0050" into an IP string and port.
// IPv4 addresses are 8 hex chars, IPv6 addresses are 32 hex chars.
func parseAddress(s string) (string, uint16, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid address format: %s", s)
	}

	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, fmt.Errorf("parsing port: %w", err)
	}

	ipHex := parts[0]
	var ip string

	switch len(ipHex) {
	case 8:
		// IPv4: stored in network byte order (little-endian on x86).
		// 0100007F -> 127.0.0.1
		b, err := hexToBytes(ipHex)
		if err != nil {
			return "", 0, err
		}
		ip = net.IPv4(b[3], b[2], b[1], b[0]).String()

	case 32:
		// IPv6: 4 groups of 4 bytes, each group in reversed byte order.
		// e.g., "00000000000000000000000001000000" is parsed as 4 groups:
		// group0=00000000, group1=00000000, group2=00000000, group3=01000000
		// Each group's bytes are reversed.
		raw := make([]byte, 16)
		for i := 0; i < 4; i++ {
			group := ipHex[i*8 : (i+1)*8]
			b, err := hexToBytes(group)
			if err != nil {
				return "", 0, err
			}
			// Reverse the 4 bytes within the group.
			raw[i*4+0] = b[3]
			raw[i*4+1] = b[2]
			raw[i*4+2] = b[1]
			raw[i*4+3] = b[0]
		}
		ip = net.IP(raw).String()

	default:
		return "", 0, fmt.Errorf("unexpected IP hex length %d: %s", len(ipHex), ipHex)
	}

	return ip, uint16(port), nil
}

// hexToBytes converts a hex string to bytes.
func hexToBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex string: %s", s)
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		v, err := strconv.ParseUint(s[i:i+2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid hex byte %q: %w", s[i:i+2], err)
		}
		b[i/2] = byte(v)
	}
	return b, nil
}
