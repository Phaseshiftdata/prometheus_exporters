package procnet

// Entry represents a single connection entry from /proc/net/tcp, tcp6, udp, or udp6.
type Entry struct {
	LocalIP    string
	LocalPort  uint16
	RemoteIP   string
	RemotePort uint16
	State      string // TCP state as string: "ESTABLISHED", "LISTEN", "TIME_WAIT", etc.
	Protocol   string // "tcp" or "udp"
	TxQueue    uint64
	RxQueue    uint64
	UID        uint32
	Inode      uint64
}

// tcpStates maps the kernel hex state codes to human-readable names.
var tcpStates = map[string]string{
	"01": "ESTABLISHED",
	"02": "SYN_SENT",
	"03": "SYN_RECV",
	"04": "FIN_WAIT1",
	"05": "FIN_WAIT2",
	"06": "TIME_WAIT",
	"07": "CLOSE",
	"08": "CLOSE_WAIT",
	"09": "LAST_ACK",
	"0A": "LISTEN",
	"0B": "CLOSING",
}

// stateName returns the human-readable state name for a hex state code.
func stateName(hexState string) string {
	if name, ok := tcpStates[hexState]; ok {
		return name
	}
	return "UNKNOWN"
}
