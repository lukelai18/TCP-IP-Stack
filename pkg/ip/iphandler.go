package ip

import (
	"fmt"
	"net/netip"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
)

const (
	IP_Protocol_TEST = 0
	IP_Protocol_RIP  = 200
)

func RIPHandler(hdr *ipv4header.IPv4Header, args []interface{}) {
	pkg := args[0].(*IP_RIPPkg)

	switch pkg.Command {
	case IP_RIP_REQ:
		ip_ripRsp_allEntries(hdr.Src)

	case IP_RIP_RESPONSE:
		ip_rip_update(pkg, hdr.Src)
	}

}

func TESTPkgHandler(hdr *ipv4header.IPv4Header, args []interface{}) {
	msg := args[0].(string)
	dst := args[1].(netip.Addr)
	fmt.Printf("Received test packet: Src: %s, Dst: %s, TTL: %d, Data: %s\n", hdr.Src.String(), hdr.Dst.String(), hdr.TTL-1, msg)

	if hdr.Dst != dst {
		ttl := hdr.TTL - 1
		_sendTo_interface(msg, hdr.Src, hdr.Dst, ttl, hdr.Protocol)
	} else {
		fmt.Printf("Receive message: %s at destination: %s\n", msg, hdr.Dst.String())
	}
}

func IP_RegisterRecvHandler(protocolNum uint8, callbackFunc HandlerFunc) {
	G_ipStackinfo.Register_HandlerMap[int(protocolNum)] = callbackFunc
}
