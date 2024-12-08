package comm

import (
	"fmt"
	"net/netip"

	"github.com/google/netstack/tcpip/header"
)

const (
	TcpHeaderLen         = header.TCPMinimumSize
	TcpPseudoHeaderLen   = 12
	IpProtoTcp           = header.TCPProtocolNumber
	MaxVirtualPacketSize = 1400
)

func IpToUint16(addr netip.Addr) (uint16, error) {
	if addr.Is4() { // Only for IPv4 addresses
		ipBytes := addr.As4()                                  // Get the IPv4 bytes
		return uint16(ipBytes[2])<<8 | uint16(ipBytes[3]), nil // Convert last 2 bytes to uint16
	}
	return 0, fmt.Errorf("only IPv4 addresses can be converted to uint16")
}
