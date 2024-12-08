package tcp

import (
	"encoding/binary"
	"ipstack_p1/pkg/comm"
	"net/netip"

	"github.com/google/netstack/tcpip/header"
)

func ParseTCPHeader(b []byte) header.TCPFields {
	td := header.TCP(b)
	return header.TCPFields{
		SrcPort:    td.SourcePort(),
		DstPort:    td.DestinationPort(),
		SeqNum:     td.SequenceNumber(),
		AckNum:     td.AckNumber(),
		DataOffset: td.DataOffset(),
		Flags:      td.Flags(),
		WindowSize: td.WindowSize(),
		Checksum:   td.Checksum(),
	}
}

func ComputeTCPChecksum(tcpHdr *header.TCPFields,
	sourceIP netip.Addr, destIP netip.Addr, payload []byte) uint16 {

	// Fill in the pseudo header
	pseudoHeaderBytes := make([]byte, comm.TcpPseudoHeaderLen)

	// First are the source and dest IPs.  This function only supports
	// IPv4, so make sure the IPs are IPv4 addresses
	copy(pseudoHeaderBytes[0:4], sourceIP.AsSlice())
	copy(pseudoHeaderBytes[4:8], destIP.AsSlice())

	// Next, add the protocol number and header length
	pseudoHeaderBytes[8] = uint8(0)
	pseudoHeaderBytes[9] = uint8(comm.IpProtoTcp)

	totalLength := comm.TcpHeaderLen + len(payload)
	binary.BigEndian.PutUint16(pseudoHeaderBytes[10:12], uint16(totalLength))

	// Turn the TcpFields struct into a byte array
	headerBytes := header.TCP(make([]byte, comm.TcpHeaderLen))
	headerBytes.Encode(tcpHdr)

	// Compute the checksum for each individual part and combine To combine the
	// checksums, we leverage the "initial value" argument of the netstack's
	// checksum package to carry over the value from the previous part
	pseudoHeaderChecksum := header.Checksum(pseudoHeaderBytes, 0)
	headerChecksum := header.Checksum(headerBytes, pseudoHeaderChecksum)
	fullChecksum := header.Checksum(payload, headerChecksum)

	// Return the inverse of the computed value,
	// which seems to be the convention of the checksum algorithm
	// in the netstack package's implementation
	return fullChecksum ^ 0xffff
}
