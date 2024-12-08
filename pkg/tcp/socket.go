package tcp

import (
	"crypto/rand"
	"fmt"
	"io"
	"ipstack_p1/pkg/comm"
	"ipstack_p1/pkg/ip"
	"log"
	"math/big"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/google/netstack/tcpip/header"
)

const (
	CLOSED       = 0
	LISTEN       = 1
	SYN_SENT     = 2
	SYN_RECEIVED = 3
	ESTABLISHED  = 4

	CLOSE_WAIT = 5
	CLOSING    = 6
	LAST_ACK   = 7
	FIN_WAIT_1 = 8
	FIN_WAIT_2 = 9
	TIME_WAIT  = 10
	/* Reserved for more */

	HANDSHAKE_WAIT_TRY = 3
)

const (
	MAX_BUFFER_SZ = 65535
	MIN_PORT      = 20000
	MAX_PORT      = 60000

	SEQ_RANGE           = 65535
	DEFAULT_WINDOW_SIZE = 65535

	MAX_SEGMENT_CHUNK = 1360

	MSL = 6

	RTT_ALPHA = 0.8    // Smoothing factor for SRTT
	RTT_BETA  = 1.5    // Multiplier for RTO
	MIN_RTO   = 10.0   // Minimum RTO in milliseconds
	MAX_RTO   = 1000.0 // Maximum RTO in milliseconds
)

type socket_key struct {
	LocalAddr  string
	LocalPort  uint16
	RemoteAddr string
	RemotePort uint16
}

type tcpPkg struct {
	tcphdr  header.TCPFields
	payload []byte
	sokk    socket_key
}

type ListenSocket struct {
	lsnsocket *Socket
}

type Socket struct {
	tcpState int
	tcpSid   int

	sokkey        *socket_key
	tcpPkgChannel chan *tcpPkg

	/* read and write for tcp stack */
	socketISN  uint16
	SND_UNA    uint32
	SND_NXT    uint32
	SND_LBW    uint32
	SND_WINDOW uint16

	RCV_NXT    uint32
	RCV_LBR    uint32
	RCV_WINDOW uint16

	SendBuffer *circularBuffer
	RcvBuffer  *circularBuffer

	/* Buffer Space conditional mutex */
	SndBufavailCond sync.Cond
	RcvBufavailCond sync.Cond
	SndreadyCond    sync.Cond
	SndWindowCond   sync.Cond

	/* ResentQ */
	ResentQ     *comm.PriorityQueue[*resentPkg, int64]
	ResentQCond sync.Cond
	ResentQLock sync.RWMutex
	ResentRTO   float64
	RTOTimer    *time.Ticker
	RTOstartRUN bool
	SRTT        float64

	/* Priority Queue for early arrival */
	EarlyArrivalQ *comm.PriorityQueue[*tcpPkg, int64]

	/* zero window probing */
	ZeroWNDChan chan bool
	ZeroProbing bool
	ZeroWNDWait chan bool

	/* close */
	SndEmptyCond sync.Cond
	TimeReset    chan bool
	CloseCheck   chan bool
}

func _GetKey_FromTCPHdr(tcpHdr header.TCPFields, sourceIp netip.Addr, destIp netip.Addr) socket_key {
	return socket_key{
		LocalAddr:  destIp.String(),
		LocalPort:  tcpHdr.DstPort,
		RemoteAddr: sourceIp.String(),
		RemotePort: tcpHdr.SrcPort,
	}
}

func _Generate_Sokk(srcip netip.Addr, dstip netip.Addr, srcp uint16, dstp uint16) socket_key {
	return socket_key{
		LocalAddr:  srcip.String(),
		LocalPort:  srcp,
		RemoteAddr: dstip.String(),
		RemotePort: dstp,
	}
}

func _Getkey_Listen(lport uint16) socket_key {
	return socket_key{
		LocalAddr:  "0.0.0.0",
		LocalPort:  lport,
		RemoteAddr: "0.0.0.0",
		RemotePort: 0,
	}
}

func _CreateListenSocket(listenK socket_key, sid int) *Socket {
	return &Socket{
		tcpState:  LISTEN,
		tcpSid:    sid,
		sokkey:    &listenK,
		socketISN: 0,

		tcpPkgChannel: make(chan *tcpPkg, 20),
		TimeReset:     make(chan bool, 10),
	}
}

func _CreateNormalSocket(norK socket_key, sid int, state int) *Socket {
	return &Socket{
		tcpState: state,
		tcpSid:   sid,
		sokkey:   &norK,

		tcpPkgChannel: make(chan *tcpPkg, 20),

		SndBufavailCond: *sync.NewCond(&sync.Mutex{}),
		RcvBufavailCond: *sync.NewCond(&sync.Mutex{}),
		SndreadyCond:    *sync.NewCond(&sync.Mutex{}),
		SndWindowCond:   *sync.NewCond(&sync.Mutex{}),
		ResentQCond:     *sync.NewCond(&sync.Mutex{}),

		SendBuffer: InitBuffer(),
		RcvBuffer:  InitBuffer(),

		EarlyArrivalQ: comm.New[*tcpPkg, int64](comm.MinHeap),
		ResentQ:       comm.New[*resentPkg, int64](comm.MinHeap),
		ResentRTO:     50.0, /* To do update: 100 ms */
		RTOTimer:      time.NewTicker(time.Duration(100.0 * float64(time.Millisecond))),
		RTOstartRUN:   false,

		ZeroWNDChan: make(chan bool),
		ZeroProbing: false,
		ZeroWNDWait: make(chan bool),

		SndEmptyCond: *sync.NewCond(&sync.Mutex{}),
		TimeReset:    make(chan bool, 10),
		CloseCheck:   make(chan bool),
	}
}

func _AddToSocketTable(listenK socket_key, sid int, sok *Socket) {
	g_TCPStack.sokTableMutex.Lock()

	g_TCPStack.sidTable[sid] = listenK
	g_TCPStack.socketTable[listenK] = sok

	g_TCPStack.sokTableMutex.Unlock()
}

/* Util help functions for socket */
func _GetISN_Number() uint16 {
	n, err := rand.Int(rand.Reader, big.NewInt(SEQ_RANGE))
	if err != nil {
		fmt.Println("Error generating random number:", err)
	}
	return uint16(n.Int64())
}

func _Generate_Port() uint16 {
	numrange := MAX_PORT - MIN_PORT + 1

regenerate:
	n, err := rand.Int(rand.Reader, big.NewInt(int64(numrange)))
	if err != nil {
		fmt.Println("Error generating random number:", err)
	}

	port := uint16(n.Int64()) + MIN_PORT
	if g_TCPStack.portTable[port] {
		goto regenerate
	} else {
		g_TCPStack.portTable[port] = true
	}

	return port
}

func _Init_RWNumber(norSocket *Socket, send uint32, rcv uint32, winsz uint16) {
	norSocket.socketISN = uint16(send)
	norSocket.SND_UNA = send
	norSocket.SND_NXT = send + 1
	norSocket.SND_LBW = send
	norSocket.SND_WINDOW = winsz

	norSocket.RCV_LBR = rcv
	norSocket.RCV_NXT = rcv + 1
	norSocket.RCV_WINDOW = DEFAULT_WINDOW_SIZE
}

func _Init_WNumber(norSocket *Socket, send uint32) {
	norSocket.socketISN = uint16(send)
	norSocket.SND_UNA = send
	norSocket.SND_NXT = send + 1
	norSocket.SND_LBW = send

	norSocket.RCV_WINDOW = DEFAULT_WINDOW_SIZE
}

func _Init_RNumber(norSocket *Socket, rcv uint32, winsz uint16) {
	norSocket.RCV_LBR = rcv
	norSocket.RCV_NXT = rcv + 1

	norSocket.SND_WINDOW = winsz
}

/* Socket function call */
func _HandShake_Second(norSocket *Socket, tcppkg *tcpPkg) bool {
	/* send syn + ack for the second handshake */
	_send_synack_back(norSocket, tcppkg)

	/* wait for 1 sec */
	time := time.NewTimer(1 * time.Second)
	for {
		select {
		case <-time.C:
			/* out of time: try again */
			log.Println("Second Handshake: out of time and try again")
			return false
		case tcppkg := <-norSocket.tcpPkgChannel:
			/* receive tcp pkg again */
			/* flag should be ack */
			if tcppkg.tcphdr.Flags&header.TCPFlagAck == 0 {
				continue
			}

			/* it is the ack from sender */
			/* 1) check seq and ack */
			headSeq := tcppkg.tcphdr.SeqNum
			headAck := tcppkg.tcphdr.AckNum
			if headSeq == uint32(norSocket.RCV_NXT) && headAck == uint32(norSocket.SND_NXT) {
				/* 2) correct: update */
				norSocket.SND_UNA = norSocket.SND_NXT
				norSocket.SND_LBW = norSocket.SND_NXT

				norSocket.RCV_LBR = norSocket.RCV_NXT
				norSocket.tcpState = ESTABLISHED

				return true
			} else {
				/* 3) error: wait for the nxt package */
				continue
			}
		}
	}

}

func _Send_Connection(norSocket *Socket) bool {
	/* send first syn req */
	_send_syn(norSocket)

	/* wait for 1 sec */
	time := time.NewTimer(1 * time.Second)
	for {
		select {
		case <-time.C:
			/* out of time: try again */
			log.Println("Third Handshake: out of time and try again")
			return false
		case tcppkg := <-norSocket.tcpPkgChannel:
			/* receive tcp pkg again */
			/* flag should be syn + ack */
			if tcppkg.tcphdr.Flags&header.TCPFlagSyn == 0 {
				continue
			}
			if tcppkg.tcphdr.Flags&header.TCPFlagAck == 0 {
				continue
			}

			/* 1) check seq and ack */
			headSeq := tcppkg.tcphdr.SeqNum /* 35248 */
			headAck := tcppkg.tcphdr.AckNum /* 14966 */

			if headAck == uint32(norSocket.SND_NXT) {
				/* 2) correct: update */
				_Init_RNumber(norSocket, headSeq, tcppkg.tcphdr.WindowSize)

				_send_ack_to_synack(norSocket, tcppkg)

				norSocket.SND_UNA = norSocket.SND_NXT
				norSocket.SND_LBW = norSocket.SND_NXT

				norSocket.RCV_LBR = norSocket.RCV_NXT
				norSocket.tcpState = ESTABLISHED
				return true
			}
		}
	}
}

func (lsn *ListenSocket) VAccept() *Socket {
	/* infinite for-loop until a normal socket is created */
	for {
		tcpPKG := <-lsn.lsnsocket.tcpPkgChannel

		if tcpPKG.tcphdr.Flags != header.TCPFlagSyn {
			/* flag should be Syn */
			continue
		}

		sid := _sidGet()
		sokk := tcpPKG.sokk
		norSocket := _CreateNormalSocket(sokk, sid, SYN_RECEIVED)
		_AddToSocketTable(sokk, sid, norSocket)

		/* get a random number for isn */
		isn := _GetISN_Number()

		/* init seq number such as lbr, lbw, una... */
		_Init_RWNumber(norSocket, uint32(isn), tcpPKG.tcphdr.SeqNum, tcpPKG.tcphdr.WindowSize)

		for i := 0; i < HANDSHAKE_WAIT_TRY; i++ {
			if _HandShake_Second(norSocket, tcpPKG) {
				go _Socket_HandlePkg(norSocket)
				go _Socket_SendThread(norSocket)
				go _Socket_HandlerResentQ(norSocket)

				norSocket.RTOTimer.Stop()
				go _Socket_Retranmission(norSocket)

				fmt.Printf("new norsocket is created with sid %d\n", sid)
				return norSocket
			}
		}

		/* Handshake fail */
		log.Printf("Error: Timeout at the second handshake\n")
		/* Send to sid delete channel */
		g_TCPStack.delSID_Channel <- sid
	}
}

func VConnect(dstAddr netip.Addr, dstPort uint16) *Socket {
	srcAddr := ip.Get_IPAddr()
	srcPort := _Generate_Port()
	sokk := _Generate_Sokk(srcAddr, dstAddr, srcPort, dstPort)

	sid := _sidGet()
	norSocket := _CreateNormalSocket(sokk, sid, SYN_SENT)
	_AddToSocketTable(sokk, sid, norSocket)

	/* get a random number for isn */
	isn := _GetISN_Number()

	/* init seq number for write side */
	_Init_WNumber(norSocket, uint32(isn))

	for i := 0; i < HANDSHAKE_WAIT_TRY; i++ {
		if _Send_Connection(norSocket) {
			go _Socket_HandlePkg(norSocket)
			go _Socket_SendThread(norSocket)
			go _Socket_HandlerResentQ(norSocket)

			norSocket.RTOTimer.Stop()
			go _Socket_Retranmission(norSocket)

			fmt.Printf("new norsocket is created with sid %d\n", sid)
			return norSocket
		}
	}

	log.Printf("Error: Fail to create normal socket")
	g_TCPStack.delSID_Channel <- sid
	return nil
}

func VListen(port uint16) *ListenSocket {
	found := g_TCPStack.portTable[port]
	if found {
		log.Println("Error: Port is already existed")
		return nil
	} else {
		/* add to port table */
		g_TCPStack.portTable[port] = true
	}

	lsnkey := _Getkey_Listen(port) /* get listen key */
	sid := _sidGet()               /* assign sid */
	lsnSok := _CreateListenSocket(lsnkey, sid)
	_AddToSocketTable(lsnkey, sid, lsnSok)
	fmt.Printf("Listen socket is created with sid %d\n", sid)

	return &ListenSocket{
		lsnsocket: lsnSok,
	}
}

func (socket *Socket) VWrite(data []byte) {
	toWrite := len(data)
	index := 0
	for toWrite > 0 {
		bufAva := _Get_SndBufSpace(socket)
		// fmt.Printf("avaliable buffer size is %d\n", bufAva)
		socket.SndBufavailCond.L.Lock()
		for bufAva == 0 {
			socket.SndBufavailCond.Wait()
			bufAva = _Get_SndBufSpace(socket)
		}
		socket.SndBufavailCond.L.Unlock()

		toput := min(int(bufAva), toWrite)
		socket.SendBuffer.Put(data[index : index+int(toput)])
		/* update buffer */
		socket.SND_LBW += uint32(toput)
		index += toput
		toWrite -= toput

		/* update receive window */
		socket.RCV_WINDOW = _Get_RcvBufSpace(socket)

		/* signal sending thread to start sending */
		socket.SndreadyCond.L.Lock()
		socket.SndreadyCond.Signal()
		socket.SndreadyCond.L.Unlock()
	}
}

func (socket *Socket) VRead(buf []byte) (int, error) {
	toRead := _Get_ReadavailabBytes(socket)

	if socket.tcpState == CLOSED || socket.tcpState == CLOSE_WAIT {
		return 0, io.EOF
	}

	socket.RcvBufavailCond.L.Lock()
	for toRead == 0 {
		socket.RcvBufavailCond.Wait()
		if socket.tcpState == CLOSED || socket.tcpState == CLOSE_WAIT {
			return 0, io.EOF
		}
		toRead = _Get_ReadavailabBytes(socket)
	}
	socket.RcvBufavailCond.L.Unlock()

	getBytes := min(len(buf), int(toRead))
	socket.RcvBuffer.Get(buf[:getBytes])
	socket.RCV_LBR += uint32(getBytes)

	return getBytes, nil
}

func _SendingFile(sok *Socket, filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		VClose(sok) // Attempt to close the connection gracefully
		return
	}
	defer file.Close()

	buf := make([]byte, MAX_BUFFER_SZ)
	total := 0

	for {
		// Read from the file
		bRead, err := file.Read(buf)
		if err != nil {
			if err == io.EOF {
				fmt.Printf("Finished sending file. Total bytes sent: %d\n", total)
				break
			} else {
				log.Printf("Error reading from file: %v\n", err)
				VClose(sok)
				return
			}
		}

		// Handle partial writes to the TCB
		sok.VWrite(buf[:bRead])
		total += bRead
		fmt.Printf("Write %d bytes in sender\n", total)
	}

	go VClose(sok)
}

// func _CheckClose(socket *Socket) {
// 	for range socket.CloseCheck {
// 		if socket.tcpState == CLOSE_WAIT {
// 			fmt.Println("Sent Close")
// 			VClose(socket)
// 		}
// 	}
// }

func _ReceivingFile(sok *Socket, dest string) {
	// Open the file for writing
	file, err := os.Create(dest)
	if err != nil {
		VClose(sok)
		return
	}
	defer file.Close()

	buf := make([]byte, MAX_BUFFER_SZ)
	total := 0

	// go _CheckClose(sok)

	for {
		numBytes, err := sok.VRead(buf)
		// fmt.Printf("Start to reading %d bytes\n", numBytes)
		if err == io.EOF {
			fmt.Printf("File received successfully. Total bytes received: %d\n", total)
			break
		}

		if numBytes == 0 {
			// No more data to read
			fmt.Printf("File received successfully. Total bytes received: %d\n", total)
			break
		}
		file.Write(buf[:numBytes])
		total += numBytes

		// fmt.Printf("Read total %d Bytest from the reader\n", total)
	}

	go VClose(sok)

}
