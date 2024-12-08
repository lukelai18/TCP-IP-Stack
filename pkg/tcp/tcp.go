package tcp

import (
	"fmt"
	"ipstack_p1/pkg/comm"
	"ipstack_p1/pkg/ip"
	"sync"
)

const (
	help_INFO       = true
	help_BufferInfo = true
	help_ResentQ    = true
)

type tcp_info struct {
	socketTable map[socket_key]*Socket /* socket map */
	sidTable    map[int]socket_key     /* sid table*/
	portTable   map[uint16]bool        /* check port is bind */

	nxtSID        int
	sokTableMutex sync.RWMutex

	delSID_Channel chan int /* remove a sid */
}

var g_TCPStack *tcp_info

func TcpStack_Init() {
	// str := "long_string_long_test"
	// b := []byte(str)
	// fmt.Printf("after string is %s\n", string(b[len(str)-1:]))

	// b2 := b[len(str):]
	// fmt.Printf("size is %d\n", len(b2))

	// str2 := "hello_newnews"
	// b2 := []byte(str2)
	// b = append(b, b2...)
	// fmt.Printf("%s\n", string(b))

	// for {
	// 	time.Sleep(5 * time.Second)
	// }

	g_TCPStack = _tcpGlobal_init()
	_tcp_regis()
}

func _tcpGlobal_init() *tcp_info {
	return &tcp_info{
		socketTable: make(map[socket_key]*Socket),
		sidTable:    make(map[int]socket_key),
		portTable:   make(map[uint16]bool),

		nxtSID:         0,
		delSID_Channel: make(chan int, 1),
	}
}

func _tcp_regis() {
	ip.IP_RegisterRecvHandler(uint8(comm.IpProtoTcp), TCPPkgHandler)
}

func _sidGet() int {
	nxtid := g_TCPStack.nxtSID
	g_TCPStack.nxtSID++
	return nxtid
}

/* helper function for printing */
func _help_printrw(sok *Socket) {
	if help_INFO {
		fmt.Printf(" \nSND_NXT: %d, SND_UNA: %d, SND_LBW: %d\n", sok.SND_NXT, sok.SND_UNA, sok.SND_LBW)
		fmt.Printf(" \nRCV_NXT: %d, RCV_LBR: %d\n", sok.RCV_NXT, sok.RCV_LBR)
		fmt.Printf(" \nRCV_WIND: %d, SEND_WIND: %d\n", sok.RCV_WINDOW, sok.SND_WINDOW)
	}
}

func _help_buffer(sok *Socket) {
	if help_BufferInfo {
		fmt.Printf("\n SNDBuffer Write: %d, Read %d\n", sok.SendBuffer.write, sok.SendBuffer.read)
		fmt.Printf("\n REVBuffer Write: %d, Read %d\n", sok.RcvBuffer.write, sok.RcvBuffer.read)
		fmt.Printf("\n Sndbuffer content: %s\n", string(sok.SendBuffer.Buffer))
		fmt.Printf("\n Rcfbuffer content: %s\n", string(sok.RcvBuffer.Buffer))
	}
}
