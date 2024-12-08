package ip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net/netip"
	"time"
)

const (
	IP_RIP_REQ      = 1
	IP_RIP_RESPONSE = 2

	IP_RIP_MAXCOST = 16
)

type IP_RIPPkg struct {
	Command    uint16
	NumEntries uint16
	Entries    []IP_RIPEntry
}

type IP_RIPEntry struct {
	Cost    uint32
	Address uint32
	Mask    uint32
}

func _periodically_check_timeout() {
	/* check the forwarding table every 1 second */
	ticker := time.NewTicker(1 * time.Second)
	for {
		<-ticker.C
		now := time.Now()

		prefixes := make([]netip.Prefix, 0)
		G_ipStackinfo.fwdMutex.Lock()
		for prefix, entry := range G_ipStackinfo.FwdTable {
			switch entry.NetType {
			case NXTHOP_ROUTER:
				if now.Sub(entry.LastUpdated) > G_ipStackinfo.RipTimeoutThreshold {
					if entry.TimeCost < IP_RIP_MAXCOST {
						entry.TimeCost = IP_RIP_MAXCOST
						prefixes = append(prefixes, prefix)
					} else {
						delete(G_ipStackinfo.FwdTable, prefix)
					}
				}
			}
		}
		G_ipStackinfo.fwdMutex.Unlock()

		if len(prefixes) != 0 {
			ip_ripTrigger_broadcast(prefixes)
		}
	}
}

func _periodically_send_response() {
	ticker := time.NewTicker(G_ipStackinfo.RipPeriodicUpdateRate)
	defer ticker.Stop() // Clean up the ticker when done

	for {
		<-ticker.C
		for _, addr := range G_ipStackinfo.RouterNeibors {
			ip_ripRsp_allEntries(addr)
		}
	}
}

func ip_rip_broadcast() {
	for _, addr := range G_ipStackinfo.RouterNeibors {
		ip_ripReq_send(addr)
	}
}

func ip_ripReq_send(routerAddr netip.Addr) {
	ripPkg := IP_RIPPkg{
		Command:    IP_RIP_REQ,
		NumEntries: 0,
		Entries:    nil, // No entries for now
	}

	bytes, err := ripPkg.ReqBytes()
	if err != nil {
		log.Println("Error in writing rip request package")
		return
	}
	_sendTo_interface_bytes(bytes, routerAddr, 32, IP_Protocol_RIP)
}

func ip_ripTrigger_broadcast(prefixarray []netip.Prefix) {
	for _, addr := range G_ipStackinfo.RouterNeibors {
		ip_ripRsp_triggerEntries(prefixarray, addr)
	}
}

func ip_ripRsp_triggerEntries(prefixarray []netip.Prefix, responseAddr netip.Addr) {
	pkg := IP_RIPPkg{
		Command:    IP_RIP_RESPONSE,        // Set Command to any initial value
		NumEntries: 0,                      // Start with zero entries
		Entries:    make([]IP_RIPEntry, 0), // Initial capacity of 0, ready for append
	}

	G_ipStackinfo.fwdMutex.Lock()
	for _, prefix := range prefixarray {
		entry := G_ipStackinfo.FwdTable[prefix]

		addrBytes := prefix.Addr().As4()
		addrUint32 := binary.BigEndian.Uint32(addrBytes[:])

		maskLength := prefix.Bits()
		maskUint32 := ^uint32(0) << (32 - maskLength)

		var timecost uint32 = 0
		if entry.NxthopAddr == responseAddr {
			timecost = IP_RIP_MAXCOST
		} else {
			timecost = uint32(entry.TimeCost)
		}

		ripentry := IP_RIPEntry{
			Cost:    timecost,
			Address: addrUint32,
			Mask:    maskUint32,
		}

		pkg.Entries = append(pkg.Entries, ripentry)
		pkg.NumEntries++
	}

	G_ipStackinfo.fwdMutex.Unlock()

	bytes, err := pkg.ReplyBytes()
	if err != nil {
		log.Println("Error in writing triggered rip response package")
		return
	}

	_sendTo_interface_bytes(bytes, responseAddr, 32, IP_Protocol_RIP)
}

func ip_ripRsp_allEntries(responseAddr netip.Addr) {
	pkg := IP_RIPPkg{
		Command:    IP_RIP_RESPONSE,        // Set Command to any initial value
		NumEntries: 0,                      // Start with zero entries
		Entries:    make([]IP_RIPEntry, 0), // Initial capacity of 0, ready for append
	}

	G_ipStackinfo.fwdMutex.Lock()
	for prefix, entry := range G_ipStackinfo.FwdTable {

		addrBytes := prefix.Addr().As4()
		addrUint32 := binary.BigEndian.Uint32(addrBytes[:])

		maskLength := prefix.Bits()
		maskUint32 := ^uint32(0) << (32 - maskLength)

		var timecost uint32 = 0
		if entry.NxthopAddr == responseAddr {
			timecost = IP_RIP_MAXCOST
		} else {
			timecost = uint32(entry.TimeCost)
		}

		ripentry := IP_RIPEntry{
			Cost:    timecost,
			Address: addrUint32,
			Mask:    maskUint32,
		}

		pkg.Entries = append(pkg.Entries, ripentry)
		pkg.NumEntries++
	}
	G_ipStackinfo.fwdMutex.Unlock()

	bytes, err := pkg.ReplyBytes()
	if err != nil {
		log.Println("Error in writing rip response package")
		return
	}

	_sendTo_interface_bytes(bytes, responseAddr, 32, IP_Protocol_RIP)
}

func ip_rip_update(pkg *IP_RIPPkg, nxthop netip.Addr) {
	if pkg.NumEntries == 0 {
		return
	}

	prefixes := make([]netip.Prefix, 0)
	G_ipStackinfo.fwdMutex.Lock()
	for _, ripentry := range pkg.Entries {
		prefix, err := formNetipPrefix(ripentry.Address, ripentry.Mask)
		if err != nil {
			log.Println("Error in forming prefix addr")
			return
		}
		fwdEntry, ok := G_ipStackinfo.FwdTable[prefix]
		new_c := ripentry.Cost + 1

		if new_c >= IP_RIP_MAXCOST {
			/* No need to update the forwarding entry */
			continue
		}

		if !ok {
			newfwdEntry := &IP_fwdTableEntry{
				NetType:    NXTHOP_ROUTER,
				NxthopName: nxthop.String(),
				NxthopAddr: nxthop,
				TimeCost:   int(new_c),

				LastUpdated: time.Now(),
			}
			G_ipStackinfo.FwdTable[prefix] = newfwdEntry
			prefixes = append(prefixes, prefix)

		} else {
			switch fwdEntry.NetType {
			case NXTHOP_ROUTER:
				if new_c < uint32(fwdEntry.TimeCost) {
					/* it is a better path */
					fwdEntry.NxthopAddr = nxthop
					fwdEntry.NxthopName = nxthop.String()
					fwdEntry.TimeCost = int(new_c)
					fwdEntry.LastUpdated = time.Now()
					prefixes = append(prefixes, prefix)
				}

				if new_c > uint32(fwdEntry.TimeCost) {
					if fwdEntry.NxthopAddr == nxthop {
						/* path changed and update the entry */
						fwdEntry.TimeCost = int(new_c)
						fwdEntry.LastUpdated = time.Now()
						prefixes = append(prefixes, prefix)
					} else {
						/* current route is better, ignore */
						/* do not update the timeout */
					}
				}

				if new_c == uint32(fwdEntry.TimeCost) && fwdEntry.NxthopAddr == nxthop {
					/* refresh the timeout */
					fwdEntry.LastUpdated = time.Now()
				}
			}
		}
	}
	G_ipStackinfo.fwdMutex.Unlock()

	if len(prefixes) != 0 {
		ip_ripTrigger_broadcast(prefixes)
	}
}

func formNetipPrefix(addr uint32, mask uint32) (netip.Prefix, error) {
	// Convert uint32 address to netip.Addr
	ipBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ipBytes, addr)
	ipAddr, ok := netip.AddrFromSlice(ipBytes)
	if !ok {
		return netip.Prefix{}, fmt.Errorf("invalid IP address")
	}

	// Convert mask to prefix length by counting bits
	prefixLen := 0
	for mask != 0 {
		prefixLen += int(mask & 1)
		mask >>= 1
	}

	// Create and return the Prefix
	return netip.PrefixFrom(ipAddr, prefixLen), nil
}

func byteToIP_RIPPkg(data []byte) (*IP_RIPPkg, error) {
	// Create a reader to read from the byte slice
	reader := bytes.NewReader(data)

	// Create an IP_RIPPkg instance
	var ripPkg IP_RIPPkg

	// Read Command
	err := binary.Read(reader, binary.BigEndian, &ripPkg.Command)
	if err != nil {
		return nil, err
	}

	// Read NumEntries
	err = binary.Read(reader, binary.BigEndian, &ripPkg.NumEntries)
	if err != nil {
		return nil, err
	}

	// Read each IP_RIPEntry based on NumEntries
	ripPkg.Entries = make([]IP_RIPEntry, ripPkg.NumEntries)
	for i := 0; i < int(ripPkg.NumEntries); i++ {
		var entry IP_RIPEntry
		err = binary.Read(reader, binary.BigEndian, &entry.Cost)
		if err != nil {
			return nil, err
		}
		err = binary.Read(reader, binary.BigEndian, &entry.Address)
		if err != nil {
			return nil, err
		}
		err = binary.Read(reader, binary.BigEndian, &entry.Mask)
		if err != nil {
			return nil, err
		}
		ripPkg.Entries[i] = entry
	}

	return &ripPkg, nil
}

func (pkg *IP_RIPPkg) ReqBytes() ([]byte, error) {
	// Create a buffer to hold the serialized data
	buf := new(bytes.Buffer)

	// Write the Command field (uint16)
	err := binary.Write(buf, binary.BigEndian, pkg.Command)
	if err != nil {
		log.Fatalf("binary.Write failed: %v", err)
		return nil, err
	}

	// Write the NumEntries field (uint16)
	err = binary.Write(buf, binary.BigEndian, pkg.NumEntries)
	if err != nil {
		log.Fatalf("binary.Write failed: %v", err)
		return nil, err
	}

	return buf.Bytes(), nil
}

func (pkg *IP_RIPPkg) ReplyBytes() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write the Command field (uint16)
	err := binary.Write(buf, binary.BigEndian, pkg.Command)
	if err != nil {
		log.Fatalf("binary.Write failed: %v", err)
		return nil, err
	}

	// Write the NumEntries field (uint16)
	err = binary.Write(buf, binary.BigEndian, pkg.NumEntries)
	if err != nil {
		log.Fatalf("binary.Write failed: %v", err)
		return nil, err
	}

	// Write each entry in the Entries slice
	for _, entry := range pkg.Entries {
		// Write Cost (uint32)
		err = binary.Write(buf, binary.BigEndian, entry.Cost)
		if err != nil {
			log.Fatalf("binary.Write failed: %v", err)
			return nil, err
		}

		// Write Address (uint32)
		err = binary.Write(buf, binary.BigEndian, entry.Address)
		if err != nil {
			log.Fatalf("binary.Write failed: %v", err)
			return nil, err
		}

		// Write Mask (uint32)
		err = binary.Write(buf, binary.BigEndian, entry.Mask)
		if err != nil {
			log.Fatalf("binary.Write failed: %v", err)
			return nil, err
		}
	}

	// The final byte slice
	return buf.Bytes(), nil
}
