package tcp

import (
	"fmt"
	"log"
	"net/netip"
)

func RCommand_call(socketID int, numBytes int) {
	if numBytes == 0 {
		fmt.Println("the number of read bytes need to be more than 1")
		return
	}

	sokk, found := g_TCPStack.sidTable[socketID]
	if found {
		socket, found := g_TCPStack.socketTable[sokk]
		if found {
			buf := make([]byte, numBytes)
			readBytes, _ := socket.VRead(buf)
			fmt.Printf("Read %d bytes: %s\n", readBytes, string(buf))
		} else {
			log.Printf("Error: no such socket with ID %d\n", socketID)
		}
	} else {
		fmt.Println("No such socket ID")
	}
}

func SCommand_call(socketID int, msg []byte) {
	sokk, found := g_TCPStack.sidTable[socketID]
	if found {
		socket, found := g_TCPStack.socketTable[sokk]
		if found {
			fmt.Printf("Start to write %d bytes data\n", len(msg))
			socket.VWrite(msg)
		} else {
			log.Printf("Error: no such socket with ID %d\n", socketID)
		}
	} else {
		fmt.Println("No such socket ID")
	}
}

func ACommand_Call(port uint16) {
	listenSocket := VListen(port)

	// Print a prompt indicating readiness for next command
	fmt.Print("> ")

	go func() {
		for {
			listenSocket.VAccept()
		}
	}()
}

func CCommand_Call(addr string, port uint16) {
	destAddr := netip.MustParseAddr(addr)
	VConnect(destAddr, port)

	// Print a prompt indicating readiness for next command
	fmt.Print("> ")
}

func _get_status_string(num int) string {
	switch num {
	case LISTEN:
		return "LISTEN"
	case SYN_RECEIVED:
		return "SYN_RECEIVED"
	case ESTABLISHED:
		return "ESTABLISHED"
	case CLOSE_WAIT:
		return "CLOSE_WAIT"
	case CLOSING:
		return "CLOSING"
	case LAST_ACK:
		return "LAST_ACK"
	case FIN_WAIT_1:
		return "FIN_WAIT_1"
	case FIN_WAIT_2:
		return "FIN_WAIT_2"
	case TIME_WAIT:
		return "TIME_WAIT"
	}
	return ""
}

func ListSockets_Call() string {
	outStr := fmt.Sprintf("%-5s %-10s %-7s %-10s %-7s %-12s\n", "SID", "LAddr", "LPort", "RAddr", "RPort", "Status")

	g_TCPStack.sokTableMutex.Lock()
	defer g_TCPStack.sokTableMutex.Unlock()

	if len(g_TCPStack.sidTable) == 0 {
		return outStr
	}

	for _, sok := range g_TCPStack.socketTable {
		outStr += fmt.Sprintf("%-5d %-10s %-7d %-10s %-7d %-12s\n", sok.tcpSid, sok.sokkey.LocalAddr, sok.sokkey.LocalPort, sok.sokkey.RemoteAddr, sok.sokkey.RemotePort, _get_status_string(sok.tcpState))
	}

	return outStr
}

func PCommand_call(socketID int) {
	/* print for debug */
	sokk, found := g_TCPStack.sidTable[socketID]
	if found {
		socket, found := g_TCPStack.socketTable[sokk]
		if found {
			_help_printrw(socket)
			_help_buffer(socket)
		} else {
			log.Printf("Error: no such socket with ID %d\n", socketID)
		}
	} else {
		fmt.Println("No such socket ID")
	}
}

func CLCommand_Call(socketID int) {
	sokk, found := g_TCPStack.sidTable[socketID]
	if found {
		socket, found := g_TCPStack.socketTable[sokk]
		if found {
			VClose(socket)
		} else {
			log.Printf("Error: no such socket with ID %d\n", socketID)
		}
	} else {
		fmt.Println("No such socket ID")
	}
}

func SFCommand_Call(ipAddr netip.Addr, portNum int, filePath string) {
	if portNum < 0 || portNum > 65535 {
		log.Println("ERROR: Invalid Port Number")
		return
	}

	socket := VConnect(ipAddr, uint16(portNum))

	go _SendingFile(socket, filePath)
}

// func RFCommand() {
// 	f, err := os.Create("./copytest.txt")
// 	if err != nil {
// 		log.Println("Can't create file")
// 		return
// 	}
// 	defer f.Close()

// 	sokk := g_TCPStack.sidTable[1]
// 	socket := g_TCPStack.socketTable[sokk]
// 	buf := make([]byte, MAX_BUFFER_SZ)

// 	total := 0
// 	readBytes := socket.VRead(buf)
// 	total += readBytes

// 	f.Write(buf[:readBytes])
// }

func RFCommand_Call(portNum int, filePath string) {
	if portNum < 0 || portNum > 65535 {
		log.Println("ERROR: Invalid Port Number")
		return
	}

	listenSok := VListen(uint16(portNum))

	sok := listenSok.VAccept()

	go _ReceivingFile(sok, filePath)
}
