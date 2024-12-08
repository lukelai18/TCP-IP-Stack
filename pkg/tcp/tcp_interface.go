package tcp

import (
	"ipstack_p1/pkg/comm"
	"ipstack_p1/pkg/ip"
	"log"
	"net/netip"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

func _tcp_pkg_send(srcPort uint16, destPort uint16, sourceIp netip.Addr, destIp netip.Addr,
	seqnum uint32, acknum uint32, flag uint8, payload []byte, winSZ uint16) []byte {
	// Build the TCP header
	tcpHdr := header.TCPFields{
		SrcPort:       srcPort,
		DstPort:       destPort,
		SeqNum:        seqnum,
		AckNum:        acknum,
		DataOffset:    20,
		Flags:         flag,
		WindowSize:    winSZ,
		Checksum:      0,
		UrgentPointer: 0,
	}

	checksum := ComputeTCPChecksum(&tcpHdr, sourceIp, destIp, payload)
	tcpHdr.Checksum = checksum

	// Serialize the TCP header
	tcpHeaderBytes := make(header.TCP, comm.TcpHeaderLen)
	tcpHeaderBytes.Encode(&tcpHdr)

	ipPacketPayload := make([]byte, 0, len(tcpHeaderBytes)+len(payload))
	ipPacketPayload = append(ipPacketPayload, tcpHeaderBytes...)
	ipPacketPayload = append(ipPacketPayload, []byte(payload)...)

	ip.Send_TCPPkg_Call(sourceIp, destIp, ipPacketPayload, int(comm.IpProtoTcp), 32)
	return ipPacketPayload
}

// handshake part_1, send syn
func _send_syn(norSocket *Socket) {
	srcport := norSocket.sokkey.LocalPort
	dstport := norSocket.sokkey.RemotePort
	srcaddr := netip.MustParseAddr(norSocket.sokkey.LocalAddr)
	dstaddr := netip.MustParseAddr(norSocket.sokkey.RemoteAddr)

	payload := []byte{}

	_tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, uint32(norSocket.socketISN),
		uint32(norSocket.RCV_NXT), header.TCPFlagSyn, payload,
		norSocket.RCV_WINDOW)
}

// handshake part_2: send syn + ack
func _send_synack_back(norSocket *Socket, tcppkg *tcpPkg) {
	srcport := tcppkg.tcphdr.DstPort
	dstport := tcppkg.tcphdr.SrcPort
	srcaddr := netip.MustParseAddr(tcppkg.sokk.LocalAddr)
	dstaddr := netip.MustParseAddr(tcppkg.sokk.RemoteAddr)

	_tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, uint32(norSocket.socketISN),
		uint32(norSocket.RCV_NXT), header.TCPFlagSyn|header.TCPFlagAck, tcppkg.payload,
		norSocket.RCV_WINDOW)
}

// handshake part_3: send ack back
func _send_ack_to_synack(norSocket *Socket, tcppkg *tcpPkg) {
	srcport := tcppkg.tcphdr.DstPort
	dstport := tcppkg.tcphdr.SrcPort
	srcaddr := netip.MustParseAddr(tcppkg.sokk.LocalAddr)
	dstaddr := netip.MustParseAddr(tcppkg.sokk.RemoteAddr)

	_tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, uint32(norSocket.SND_NXT),
		uint32(norSocket.RCV_NXT), header.TCPFlagAck, tcppkg.payload,
		norSocket.RCV_WINDOW)
}

func _send_ack_pkg(norSocket *Socket, payload []byte) []byte {
	srcport := norSocket.sokkey.LocalPort
	dstport := norSocket.sokkey.RemotePort
	srcaddr := netip.MustParseAddr(norSocket.sokkey.LocalAddr)
	dstaddr := netip.MustParseAddr(norSocket.sokkey.RemoteAddr)

	return _tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, uint32(norSocket.SND_NXT),
		uint32(norSocket.RCV_NXT), header.TCPFlagAck, payload, _Get_RcvBufSpace(norSocket),
	)
}

func _send_zwp_pkg(norSocket *Socket, payload []byte, seq uint32) {
	srcport := norSocket.sokkey.LocalPort
	dstport := norSocket.sokkey.RemotePort
	srcaddr := netip.MustParseAddr(norSocket.sokkey.LocalAddr)
	dstaddr := netip.MustParseAddr(norSocket.sokkey.RemoteAddr)
	_tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, seq,
		uint32(norSocket.RCV_NXT), header.TCPFlagAck, payload, _Get_RcvBufSpace(norSocket),
	)
}

/* close send */
func _send_fin_pkg(norSocket *Socket, payload []byte, rcv uint32) {
	srcport := norSocket.sokkey.LocalPort
	dstport := norSocket.sokkey.RemotePort
	srcaddr := netip.MustParseAddr(norSocket.sokkey.LocalAddr)
	dstaddr := netip.MustParseAddr(norSocket.sokkey.RemoteAddr)

	_tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, uint32(norSocket.SND_NXT),
		rcv, header.TCPFlagAck|header.TCPFlagFin, payload, _Get_RcvBufSpace(norSocket),
	)
}

func _send_ackfin_pkg(norSocket *Socket, payload []byte, rcv uint32) []byte {
	srcport := norSocket.sokkey.LocalPort
	dstport := norSocket.sokkey.RemotePort
	srcaddr := netip.MustParseAddr(norSocket.sokkey.LocalAddr)
	dstaddr := netip.MustParseAddr(norSocket.sokkey.RemoteAddr)

	return _tcp_pkg_send(srcport, dstport, srcaddr, dstaddr, uint32(norSocket.SND_NXT),
		rcv, header.TCPFlagAck, payload, DEFAULT_WINDOW_SIZE,
	)
}

func _tcp_receive_handler(tcpHeaderAndData []byte, srcip netip.Addr, dstip netip.Addr) {
	tcpHdr := ParseTCPHeader(tcpHeaderAndData)
	tcpPayload := tcpHeaderAndData[tcpHdr.DataOffset:]

	// Check the checksum
	tcpChecksumFromHeader := tcpHdr.Checksum
	tcpHdr.Checksum = 0
	tcpComputedChecksum := ComputeTCPChecksum(&tcpHdr, srcip, dstip, tcpPayload)

	if tcpComputedChecksum != tcpChecksumFromHeader {
		log.Printf("TCP checksum error !\n")
		return
	}

	norkey := _GetKey_FromTCPHdr(tcpHdr, srcip, dstip)
	g_TCPStack.sokTableMutex.Lock()
	sok, found := g_TCPStack.socketTable[norkey]
	g_TCPStack.sokTableMutex.Unlock()

	tcpPackage := tcpPkg{
		tcphdr:  tcpHdr,
		payload: tcpPayload,
		sokk:    norkey,
	}

	if found {
		/* found existed socket */
		sok.tcpPkgChannel <- &tcpPackage
		return
	}

	/* it is for listen socket */
	lsnkey := _Getkey_Listen(tcpHdr.DstPort)
	g_TCPStack.sokTableMutex.Lock()
	sok, found = g_TCPStack.socketTable[lsnkey]
	g_TCPStack.sokTableMutex.Unlock()

	if found {
		/* found existed listen socket */
		sok.tcpPkgChannel <- &tcpPackage
	} else {
		log.Println("Error: listen socket isn't created yet")
	}
}

func TCPPkgHandler(hdr *ipv4header.IPv4Header, args []interface{}) {
	payload := args[0].([]byte)
	dst := args[1].(netip.Addr)

	// If the destination is not in this interface, forward it and decrement the ttl
	if hdr.Dst != dst {
		ttl := hdr.TTL - 1
		ip.Send_TCPPkg_Call(hdr.Src, hdr.Dst, payload, hdr.Protocol, ttl)
	} else {
		/* payload is tcp hdr + tcp content */
		_tcp_receive_handler(payload, hdr.Src, hdr.Dst)
	}
}
