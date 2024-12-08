package ip

import (
	"fmt"
	lnxconfig "ipstack_p1/pkg/Inxconfig"
	"ipstack_p1/pkg/comm"
	"log"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
)

const (
	IP_Version_4 = 4

	IP_PACKAGE_SZ = 10
	IP_MTU_SZ     = 1500
	IP_MAXMSG_SZ  = 1400
)

const (
	NXTHOP_ROUTER    = 0
	NXTHOP_INTERFACE = 1
)

type HandlerFunc = func(*ipv4header.IPv4Header, []interface{})

/* Forwarding Table */
type IP_StackInfo struct {
	FwdTable     map[netip.Prefix]*IP_fwdTableEntry /* forwarding table */
	InterfaceMap map[string]*IP_interface           /* interface table */
	// RecMsgChannel       chan *_ip_ReceiPkg
	Register_HandlerMap map[int]HandlerFunc

	fwdMutex      sync.RWMutex
	DeviceType    lnxconfig.RoutingMode
	RouterNeibors []netip.Addr

	RipPeriodicUpdateRate time.Duration
	RipTimeoutThreshold   time.Duration
}

type IP_fwdTableEntry struct {
	NetType    int
	NxthopName string
	NxthopAddr netip.Addr
	TimeCost   int

	LastUpdated time.Time
}

type IP_interface struct {
	// Name is as the key
	State bool

	Addr      netip.Addr
	Prefix    netip.Prefix
	UDPAddr   netip.AddrPort
	UDPConn   *net.UDPConn
	Neighbors map[netip.Addr]IP_ifaceNeighbor

	SendChan chan *_ip_SendPkg
	LocalUDP *net.UDPAddr
}

type IP_ifaceNeighbor struct {
	UDPAddr   netip.AddrPort
	UDPRemote *net.UDPAddr
}

type _ip_SendPkg struct {
	/* ipheader + message bytes */
	ipMsgbytes []byte
	/* destination for ifh to get in neighbors */
	dstAddr netip.Addr
}

// type _ip_ReceiPkg struct {
// 	msg          string
// 	pkg_protocol int
// }

var G_ipStackinfo *IP_StackInfo

func IPStack_Init(ipconfig *lnxconfig.IPConfig) {
	/* 1. Init IP stack info */
	G_ipStackinfo = _ipGlobal_init(ipconfig)

	/* 2. create forwarding and interfacetable */
	_fwdTable_Create(ipconfig)
	err := _ifaceTable_Create(ipconfig)
	if err != nil {
		log.Println("Error in creating interface table in ip stack")
		panic(1)
	}

	/* 3. init routers */
	_router_init(ipconfig)

	/* 4. Create a goroutine for each interface */
	_interface_Create()

}

func _ipGlobal_init(ipconfig *lnxconfig.IPConfig) *IP_StackInfo {
	return &IP_StackInfo{
		FwdTable:     make(map[netip.Prefix]*IP_fwdTableEntry),
		InterfaceMap: make(map[string]*IP_interface),
		// RecMsgChannel:       make(chan *_ip_ReceiPkg, IP_PACKAGE_SZ),
		Register_HandlerMap: make(map[int]HandlerFunc),

		RouterNeibors:         make([]netip.Addr, 0),
		DeviceType:            ipconfig.RoutingMode,
		RipPeriodicUpdateRate: ipconfig.RipPeriodicUpdateRate,
		RipTimeoutThreshold:   ipconfig.RipTimeoutThreshold,
	}
}

/* Forwarding Table functions */
func _fwdTable_Create(ipconfig *lnxconfig.IPConfig) {
	/* Add interface element to forwarding table */
	for _, iface := range ipconfig.Interfaces {
		fwdEntry := IP_fwdTableEntry{
			NxthopName: iface.Name,
			NxthopAddr: iface.AssignedIP,
			NetType:    NXTHOP_INTERFACE,
			TimeCost:   0,

			// LastUpdated: time.Now(),
		}
		G_ipStackinfo.FwdTable[iface.AssignedPrefix] = &fwdEntry
	}

	/* Add router element to forwarding table */
	for ipPrefix, ipAddr := range ipconfig.StaticRoutes {
		fwdEntry := IP_fwdTableEntry{
			NxthopName: ipAddr.String(),
			NxthopAddr: ipAddr,
			NetType:    NXTHOP_ROUTER,
			TimeCost:   1,

			LastUpdated: time.Now(),
		}
		G_ipStackinfo.FwdTable[ipPrefix] = &fwdEntry
	}
}

func _ifaceTable_Create(ipconfig *lnxconfig.IPConfig) error {
	for _, interface_Config := range ipconfig.Interfaces {

		/* Create a local udp conn for each interface */
		localUDPAddr, err := net.ResolveUDPAddr("udp4", interface_Config.UDPAddr.String())

		if err != nil {
			log.Panicln("Error resolving address:  ", err)
			return err
		}

		bindConn, err := net.ListenUDP("udp4", localUDPAddr)
		if err != nil {
			log.Panicln("Error binding address:  ", err)
			return err
		}

		/* init each interface in a ip stack */
		ipInterface := IP_interface{
			State:     true,
			Addr:      interface_Config.AssignedIP,
			Prefix:    interface_Config.AssignedPrefix,
			UDPAddr:   interface_Config.UDPAddr,
			UDPConn:   bindConn,
			Neighbors: make(map[netip.Addr]IP_ifaceNeighbor),
			SendChan:  make(chan *_ip_SendPkg, 5),
			LocalUDP:  localUDPAddr,
		}

		G_ipStackinfo.InterfaceMap[interface_Config.Name] = &ipInterface
	}

	/* init all neighboors in ip stack */
	for _, neighbor := range ipconfig.Neighbors {

		remoteUDPAddr, err := net.ResolveUDPAddr("udp4", neighbor.UDPAddr.String())
		if err != nil {
			log.Panicln("Error resolving neighbor address: ", err)
			return err
		}

		ipNeignbor := IP_ifaceNeighbor{
			UDPAddr:   neighbor.UDPAddr,
			UDPRemote: remoteUDPAddr,
		}

		ipInterface := G_ipStackinfo.InterfaceMap[neighbor.InterfaceName]
		ipInterface.Neighbors[neighbor.DestAddr] = ipNeignbor
	}

	return nil
}

func longestPrefixMatch(addr netip.Addr) (netip.Prefix, bool) {
	var longestMatch netip.Prefix
	found := false

	for prefix := range G_ipStackinfo.FwdTable {
		if prefix.Contains(addr) {
			// Check if this prefix is longer (more specific) than the current match
			if !found || prefix.Bits() > longestMatch.Bits() {
				longestMatch = prefix
				found = true
			}
		}
	}

	return longestMatch, found
}

func _fwdTbale_Lookup(addr netip.Addr) (bool, string, netip.Addr) {
	G_ipStackinfo.fwdMutex.Lock()
	defer G_ipStackinfo.fwdMutex.Unlock()
	searchAddr := addr

	for {
		prefix, found := longestPrefixMatch(searchAddr)
		if !found {
			log.Printf("Unable to map the prefix: %s in the forwarfing table\n", prefix.String())
			return found, "", netip.Addr{}
		} else {
			ipInterface := G_ipStackinfo.FwdTable[prefix]
			switch ipInterface.NetType {
			case NXTHOP_INTERFACE:
				return found, ipInterface.NxthopName, searchAddr
			case NXTHOP_ROUTER:
				searchAddr = ipInterface.NxthopAddr
			}
		}
	}
}

/* Interface functions */
func _interface_Create() {
	for interfaceName, ipInterface := range G_ipStackinfo.InterfaceMap {
		// fmt.Printf("Create a interface: %s\n", interfaceName)
		go _interface_handler(interfaceName, ipInterface)
	}
}

func _interface_handler(ifhName string, ipinterface *IP_interface) {
	go func() {
		for {
			buffer := make([]byte, IP_MAXMSG_SZ)
			n, _, err := ipinterface.UDPConn.ReadFromUDP(buffer) /*127.0.0.1:5001*/

			if !ipinterface.State {
				// fmt.Println("interface receive closed")
				continue
			}

			if err != nil {
				log.Panicln("Error reading from UDP socket ", err)
			}

			/* TO DO: check the ip package (TTL, checksum, etc...) */
			if n == 0 {
				continue
			}
			data := make([]byte, n)
			copy(data, buffer[:n])
			_receiveFrom_interface(data, ipinterface)
		}
	}()

	for ipMsg := range ipinterface.SendChan {
		conn := ipinterface.UDPConn
		n, found := ipinterface.Neighbors[ipMsg.dstAddr]
		if ipinterface.Addr == ipMsg.dstAddr {
			_, err := conn.WriteToUDP(ipMsg.ipMsgbytes, ipinterface.LocalUDP)
			if err != nil {
				log.Printf("Error in sending local message: %s\n", ifhName)
				continue
			}
			// fmt.Printf("Sent to local %d bytes\n", bytesWritten)

		} else if found {
			_, err := conn.WriteToUDP(ipMsg.ipMsgbytes, n.UDPRemote)
			if err != nil {
				log.Printf("Error in sending message of %s\n", ifhName)
				log.Printf("from %s to %s\n", ipinterface.Addr.String(), n.UDPRemote.String())
				continue
			}
			// fmt.Printf("Sent %d bytes\n", bytesWritten)

		} else {
			log.Printf("Error in finding the destination addr %s\n", ipMsg.dstAddr.String())
			log.Printf("Drop the package in interface: %s", ifhName)

		}
	}
}

func _sendTo_interface(message string, srcAddr netip.Addr, dstAddr netip.Addr, ttl int, pkg_protocol int) bool {
	found, ifhname, nextAddr := _fwdTbale_Lookup(dstAddr)
	if !found {
		log.Println("Error in sending message to interface")
		return found
	}
	ifh := G_ipStackinfo.InterfaceMap[ifhname] /* Get the interface structure */

	if !ifh.State {
		// fmt.Println("interface send closed")
		return false
	}

	// fmt.Printf("Dest: %s\n", dstAddr.String())
	// fmt.Printf("Src: %s\n", ifh.Addr.String())
	src := netip.Addr{}
	if src == srcAddr {
		src = ifh.Addr
	} else {
		src = srcAddr
	}

	hdr := ipv4header.IPv4Header{
		Version:  IP_Version_4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(message),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      ttl,
		Protocol: pkg_protocol,
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      src,
		Dst:      dstAddr,
		Options:  []byte{},
	}

	headerBytes, err := hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
		return false
	}

	hdr.Checksum = int(ComputeChecksum(headerBytes))

	headerBytes, err = hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
		return false
	}

	bytesToSend := make([]byte, 0, len(headerBytes)+len(message))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, []byte(message)...)

	ipMsg := _ip_SendPkg{
		ipMsgbytes: bytesToSend,
		dstAddr:    nextAddr,
	}

	ifh.SendChan <- &ipMsg
	return true
}

func _sendTo_interface_bytes(bytes []byte, dstAddr netip.Addr, ttl int, pkg_protocol int) bool {
	found, ifhname, nextAddr := _fwdTbale_Lookup(dstAddr)
	if !found {
		log.Println("Error in sending message to interface")
		return found
	}
	ifh := G_ipStackinfo.InterfaceMap[ifhname] /* Get the interface structure */

	if !ifh.State {
		// fmt.Println("interface send closed")
		return false
	}
	// fmt.Printf("Dest: %s\n", dstAddr.String())
	// fmt.Printf("Src: %s\n", ifh.Addr.String())

	hdr := ipv4header.IPv4Header{
		Version:  IP_Version_4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(bytes),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      ttl,
		Protocol: pkg_protocol,
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      ifh.Addr,
		Dst:      dstAddr,
		Options:  []byte{},
	}

	headerBytes, err := hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
		return false
	}

	hdr.Checksum = int(ComputeChecksum(headerBytes))

	headerBytes, err = hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
		return false
	}

	bytesToSend := make([]byte, 0, len(headerBytes)+len(bytes))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, bytes...)

	ipMsg := _ip_SendPkg{
		ipMsgbytes: bytesToSend,
		dstAddr:    nextAddr,
	}

	ifh.SendChan <- &ipMsg
	return true
}

func _sendTo_interface_bytes_tcp(bytes []byte, srcAddr netip.Addr, dstAddr netip.Addr, ttl int, pkg_protocol int) bool {
	found, ifhname, nextAddr := _fwdTbale_Lookup(dstAddr)
	if !found {
		log.Println("Error in sending message to interface")
		return found
	}
	ifh := G_ipStackinfo.InterfaceMap[ifhname] /* Get the interface structure */

	if !ifh.State {
		// fmt.Println("interface send closed")
		return false
	}

	hdr := ipv4header.IPv4Header{
		Version:  IP_Version_4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(bytes),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      ttl,
		Protocol: pkg_protocol,
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      srcAddr,
		Dst:      dstAddr,
		Options:  []byte{},
	}

	headerBytes, err := hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
		return false
	}

	hdr.Checksum = int(ComputeChecksum(headerBytes))

	headerBytes, err = hdr.Marshal()
	if err != nil {
		log.Fatalln("Error marshalling header:  ", err)
		return false
	}

	// Assign the space and append the header and payload
	bytesToSend := make([]byte, 0, len(headerBytes)+len(bytes))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, bytes...)

	ipMsg := _ip_SendPkg{
		ipMsgbytes: bytesToSend,
		dstAddr:    nextAddr,
	}

	ifh.SendChan <- &ipMsg
	return true
}

func _receiveFrom_interface(data []byte, ipinterface *IP_interface) {
	// Parse IP header
	hdr, err := ipv4header.ParseHeader(data)
	if err != nil {
		// Invalid IP header, drop the packet
		log.Println("Error parsing IP header:", err)
		return
	}

	headerSize := hdr.Len
	if headerSize > len(data) {
		// Header length exceeds data length, invalid packet
		log.Println("Invalid header length")
		return
	}

	// Check the TTL
	if hdr.TTL <= 0 {
		log.Println("TTL Expired")
		return
	}

	// Validate the checksum'
	headerBytes := data[:headerSize]
	checksumFromHeader := uint16(hdr.Checksum)
	computedChecksum := ValidateChecksum(headerBytes, checksumFromHeader)
	if computedChecksum != checksumFromHeader {
		log.Println("Invalid checksum")
		return
	}

	// fmt.Printf("Receive Dest: %s\n", hdr.Dst.String())
	// fmt.Printf("Receive Src: %s\n", hdr.Src.String())
	// fmt.Printf("Current ifh addr %s\n", ipinterface.Addr.String())

	msg := string(data[headerSize:])
	/* depend on the protocol number  */
	handler, found := G_ipStackinfo.Register_HandlerMap[hdr.Protocol]
	if !found {
		fmt.Println("Unsupported protocol type")
		return
	} else {
		switch hdr.Protocol {
		case IP_Protocol_TEST:
			args := []interface{}{msg, ipinterface.Addr}
			handler(hdr, args)
		case IP_Protocol_RIP:
			rippkg, err := byteToIP_RIPPkg(data[headerSize:])
			if err != nil {
				log.Printf("%s", err)
				log.Printf("Error in receive rippkg")
				return
			}
			args := []interface{}{rippkg}
			handler(hdr, args)
		case int(comm.IpProtoTcp):
			args := []interface{}{data[headerSize:], ipinterface.Addr}
			handler(hdr, args)
		}
	}
}

/* init routers */
func _router_init(ipconfig *lnxconfig.IPConfig) {
	G_ipStackinfo.RouterNeibors = ipconfig.RipNeighbors
}

func IPStack_Start_Host() {
	IP_RegisterRecvHandler(IP_Protocol_TEST, TESTPkgHandler)
}

func IPStack_Start_Router() {
	IP_RegisterRecvHandler(IP_Protocol_TEST, TESTPkgHandler)
	IP_RegisterRecvHandler(IP_Protocol_RIP, RIPHandler)

	ip_rip_broadcast()
	/* periodically send rip pkgs to neighbor routers */
	go _periodically_send_response()
	go _periodically_check_timeout()
}

/* For commands used in vhost and vrouter */
func Get_IPAddr() netip.Addr {
	for _, ipInterface := range G_ipStackinfo.InterfaceMap {
		return ipInterface.Addr
	}
	return netip.Addr{}
}

func ListInterface_Call() string {
	outStr := fmt.Sprintf("%-6s %-12s %s\n", "Name", "Addr/Prefix", "State")

	for name, ipInterface := range G_ipStackinfo.InterfaceMap {
		var stateStr string
		if ipInterface.State {
			stateStr = "up"
		} else {
			stateStr = "down"
		}

		prefixLength := strconv.Itoa(ipInterface.Prefix.Bits())
		outStr += fmt.Sprintf("%-6s %-12s %s\n", name, ipInterface.Addr.String()+"/"+prefixLength, stateStr)
	}
	return outStr
}

func ListNeighbor_Call() string {
	outStr := fmt.Sprintf("%-6s %-12s %s\n", "Iface", "VIP", "UDPAddr")
	for name, ipInterface := range G_ipStackinfo.InterfaceMap {
		if !ipInterface.State {
			continue
		}

		for addr, n := range ipInterface.Neighbors {
			outStr += fmt.Sprintf("%-6s %-12s %s\n", name, addr.String(), n.UDPAddr.String())
		}
	}
	return outStr
}

func ListRouter_Call() string {
	outStr := fmt.Sprintf("%-6s %-12s %-12s %s\n", "T", "Prefix", "Next hop", "Cost")
	G_ipStackinfo.fwdMutex.Lock()
	defer G_ipStackinfo.fwdMutex.Unlock()
	for prefix, fwdEntry := range G_ipStackinfo.FwdTable {
		switch fwdEntry.NetType {
		case NXTHOP_INTERFACE:
			outStr += fmt.Sprintf("%-6s %-12s %-12s %d\n", "L", prefix.String(), "LOCAL:"+fwdEntry.NxthopName, fwdEntry.TimeCost)
		case NXTHOP_ROUTER:
			if prefix.String() == "0.0.0.0/0" {
				outStr += fmt.Sprintf("%-6s %-12s %-12s %s\n", "S", prefix.String(), fwdEntry.NxthopName, "-")
			} else {
				outStr += fmt.Sprintf("%-6s %-12s %-12s %d\n", "R", prefix.String(), fwdEntry.NxthopName, fwdEntry.TimeCost)
			}
		}
	}
	return outStr
}

func Down_Call(ifname string) {
	found := false

	for name, iFaceEntry := range G_ipStackinfo.InterfaceMap {
		if ifname == name {
			iFaceEntry.State = false
			found = true
		}
	}

	if !found {
		log.Printf("Error disabling interface %s: interface not found\n", ifname)
		return
	}

	found = false
	for _, fwdentry := range G_ipStackinfo.FwdTable {
		if ifname == fwdentry.NxthopName {
			fwdentry.TimeCost = IP_RIP_MAXCOST
			found = true
		}
	}

	if !found {
		// If no match is found, print an error message
		log.Printf("Error disabling interface %s in fwdtable: interface not found\n", ifname)
	}
}

func Up_Call(ifname string) {
	for name, iFaceEntry := range G_ipStackinfo.InterfaceMap {
		if ifname == name {
			iFaceEntry.State = true

			// Reassign the modified struct back to the map
			G_ipStackinfo.InterfaceMap[ifname] = iFaceEntry
			return
		}
	}
	// If no match is found, print an error message
	log.Printf("Error enabling interface %s: interface not found\n", ifname)
}

/* Temporary Test function */
func Send_TestPkg_Call(addr string, message string) {
	// dstAddr := netip.MustParseAddr("10.0.0.1")
	// _sendTo_interface("hello !", dstAddr, 32, 0)
	destAddr := netip.MustParseAddr(addr)
	_sendTo_interface(message, netip.Addr{}, destAddr, 32, IP_Protocol_TEST)
}

func Send_TCPPkg_Call(srcAddr netip.Addr, dstAddr netip.Addr, payload []byte, tcp_Type int, ttl int) {
	_sendTo_interface_bytes_tcp(payload, srcAddr, dstAddr, ttl, tcp_Type)
}
