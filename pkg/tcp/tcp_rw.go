package tcp

import (
	"fmt"
	"log"
	"net/netip"
	"time"

	"github.com/google/netstack/tcpip/header"
)

type resentPkg struct {
	sentTcpPkg  []byte
	sentTime    time.Time
	srcaddr     netip.Addr
	dstaddr     netip.Addr
	payloadSize int
}

/* Get readable buffer size */
func _Get_ReadavailabBytes(socket *Socket) uint16 {
	return uint16(socket.RCV_NXT - socket.RCV_LBR)
}

/* Get availableSpace of send buffer */
func _Get_SndBufSpace(socket *Socket) uint16 {
	return uint16(MAX_BUFFER_SZ - (socket.SND_LBW - socket.SND_NXT))
}

/* Get availableSpace of receive buffer */
func _Get_RcvBufSpace(socket *Socket) uint16 {
	return uint16(MAX_BUFFER_SZ - (socket.RCV_NXT - socket.RCV_LBR))
}

/* Get bytes that is ready to be sent to */
func _Get_Bytestosend(socket *Socket) uint16 {
	return uint16(socket.SND_LBW - socket.SND_NXT)
}

/* Get window size that is used to send bytes */
func _Get_WindowToSend(socket *Socket) (bool, uint16) {
	if socket.SND_UNA+uint32(socket.SND_WINDOW) < socket.SND_NXT {
		return false, 0
	} else {
		return true, uint16(socket.SND_UNA + uint32(socket.SND_WINDOW) - socket.SND_NXT)
	}
}

func _Get_Unfinished(socket *Socket) uint16 {
	return uint16(socket.SND_LBW - socket.SND_UNA)
}

func _doReceive_Payload(socket *Socket, tcppkg *tcpPkg) {
	/* check receive buffer */
	plen := len(tcppkg.payload)
	inbuf := _Get_RcvBufSpace(socket)
	off := min(plen, int(inbuf))
	socket.RcvBuffer.Put(tcppkg.payload[:off])

	socket.RCV_NXT += uint32(off)
	// log.Printf("off is %d\n", off)

	/* send ack to VWrite */
	_send_ack_pkg(socket, []byte{})

	/* wakeup VRead to get data */
	socket.RcvBufavailCond.L.Lock()
	socket.RcvBufavailCond.Signal()
	socket.RcvBufavailCond.L.Unlock()
}

func _doReceive_Ack(socket *Socket, tcppkg *tcpPkg, ACK uint32) {
	if socket.SND_UNA < ACK && ACK <= socket.SND_NXT {
		/* 1. ACK is in the expected range */
		/* 1) check una UNA___pkg1__pkg2__..._ */
		socket.SND_UNA = ACK

		/* 2. Update Send Window size */
		socket.SND_WINDOW = tcppkg.tcphdr.WindowSize
		if socket.SND_WINDOW == 0 && !socket.ZeroProbing {
			if !socket.SendBuffer.IsFull() {
				go _Socket_ZeroProbing(socket)
			} else {
				go _Socket_Waiting_ZeroProbing(socket)
			}
		}

		socket.SndWindowCond.L.Lock()
		socket.SndWindowCond.Signal()
		socket.SndWindowCond.L.Unlock()

	} else {
		if ACK > socket.SND_NXT {
			/* it is probably zero window probing ack */
			socket.SND_WINDOW = tcppkg.tcphdr.WindowSize
			if socket.SND_WINDOW > 0 && socket.ZeroProbing {
				socket.ZeroWNDChan <- true
				/* 3. notify window buffer */
				socket.SndWindowCond.L.Lock()
				socket.SndWindowCond.Signal()
				socket.SndWindowCond.L.Unlock()
				log.Println("End: Zero Window probing")
			}
		}
	}

	if socket.SND_LBW == socket.SND_UNA {
		socket.SndEmptyCond.L.Lock()
		socket.SndEmptyCond.Signal()
		socket.SndEmptyCond.L.Unlock()
	}

	/* 4. notify write buffer, there has space now */
	socket.SndBufavailCond.L.Lock()
	socket.SndBufavailCond.Signal()
	socket.SndBufavailCond.L.Unlock()

	/* 5. notify resentQ ack is updated */
	socket.ResentQCond.L.Lock()
	socket.ResentQCond.Signal()
	socket.ResentQCond.L.Unlock()

	// Check if we're in LAST_ACK state and the ACK acknowledges our FIN
	if socket.tcpState == LAST_ACK {
		// && ACK == socket.SND_NXT
		// Transition to CLOSED state
		socket.tcpState = CLOSED

		// Clean up resources, close channels, etc.
		close(socket.tcpPkgChannel)

		// Remove socket from maps
		_removeSocketFromMaps(socket)
		fmt.Printf("Socket %d closed\n", socket.tcpSid)

		socket.RcvBufavailCond.L.Lock()
		socket.RcvBufavailCond.Signal()
		socket.RcvBufavailCond.L.Unlock()

		return
	}
}

func _doPayload_Op(socket *Socket, tcppkg *tcpPkg, start uint32) {
	/* 1. modify start position */
	if start != socket.RCV_NXT {
		if start < socket.RCV_NXT && socket.RCV_NXT < start+uint32(len(tcppkg.payload)) {
			tcppkg.payload = tcppkg.payload[socket.RCV_NXT-start:]
			start = socket.RCV_NXT
		} else {
			/* it should be ignored */
			tcppkg.payload = []byte{}
			start = socket.RCV_NXT
		}
	}

	/* 2. merge payload */
	for !socket.EarlyArrivalQ.IsEmpty() {
		item := socket.EarlyArrivalQ.Get()
		off := start + uint32(len(tcppkg.payload))
		if off == uint32(item.Priority) {
			/* append queue */
			tcppkg.payload = append(tcppkg.payload, item.Value.payload[off-uint32(item.Priority):]...)
		} else {
			/* still out of order */
			socket.EarlyArrivalQ.Put(item.Value, item.Priority)
			break
		}
	}
}

/* Every package from transimit layer will be delivered to here */
func _Socket_HandlePkg(socket *Socket) {
	for tcppkg := range socket.tcpPkgChannel {
		seqN := tcppkg.tcphdr.SeqNum
		pkgLen := len(tcppkg.payload)

		/* for package arrived early */
		if seqN > socket.RCV_NXT {
			if len(tcppkg.payload) != 0 {
				socket.EarlyArrivalQ.Put(tcppkg, int64(seqN))
			}

			/* 2). send back (ack is rcv_nxt) */
			_send_ack_pkg(socket, []byte{})
			continue
		}

		/* seq < nxt but nxt is in middle of seg + LEN */
		/* merge early arrival pkg */
		_doPayload_Op(socket, tcppkg, seqN)

		if pkgLen != 0 {
			/* it is payload send to receiver */
			_doReceive_Payload(socket, tcppkg)

		} else {
			ACKnum := tcppkg.tcphdr.AckNum
			/* it is Ack send back or it is Fin */
			if tcppkg.tcphdr.Flags&header.TCPFlagFin != 0 {
				/* Fin request */
				_doReceive_FIN(socket, ACKnum)
			} else if socket.SND_NXT == ACKnum && socket.tcpState == FIN_WAIT_1 {
				socket.tcpState = FIN_WAIT_2
			} else {
				_doReceive_Ack(socket, tcppkg, ACKnum)
			}

		}
	}
}

func _Socket_SendThread(socket *Socket) {
	for {
		tosend := _Get_Bytestosend(socket)

		socket.SndreadyCond.L.Lock()
		if tosend == 0 {
			socket.SndreadyCond.Wait()
			tosend = _Get_Bytestosend(socket)
		}
		socket.SndreadyCond.L.Unlock()

		for tosend > 0 {
			/* check window size */
			/* potential bug:
			   send while it is zero window probe ?
			   window size is negative ?
			*/
			/* TO DO: Window size is negative */

			_, WINDOW := _Get_WindowToSend(socket)
			// log.Printf("WIND is %d\n", WINDOW)

			if WINDOW == 0 && socket.ZeroProbing {
				/* send signal to zero window channle */
				fmt.Println("There are bytes to probe")
				socket.ZeroWNDWait <- true
				// tosend--
			}

			socket.SndWindowCond.L.Lock()
			for WINDOW == 0 {
				socket.SndWindowCond.Wait()
				_, WINDOW = _Get_WindowToSend(socket)
				// log.Printf("WIND is %d\n", WINDOW)
			}
			socket.SndWindowCond.L.Unlock()

			avalsend := min(tosend, WINDOW, MAX_SEGMENT_CHUNK)
			// log.Printf("avalsend is %d\n", avalsend)
			buf := make([]byte, avalsend)
			socket.SendBuffer.Get(buf)

			resent_start := socket.SND_NXT
			sentpkg := _send_ack_pkg(socket, buf)
			socket.SND_NXT += uint32(avalsend)
			tosend -= avalsend

			// log.Printf("tosend is %d\n", tosend)
			/* push sent packages to a queue for retransmission */
			socket.ResentQLock.Lock()
			resentPkg := &resentPkg{
				sentTcpPkg:  sentpkg,
				sentTime:    time.Now(),
				srcaddr:     netip.MustParseAddr(socket.sokkey.LocalAddr),
				dstaddr:     netip.MustParseAddr(socket.sokkey.RemoteAddr),
				payloadSize: len(buf),
			}
			socket.ResentQ.Put(resentPkg, int64(resent_start))
			socket.ResentQLock.Unlock()

			if !socket.RTOstartRUN {
				socket.RTOstartRUN = true
				socket.RTOTimer.Reset(time.Duration(socket.ResentRTO * float64(time.Millisecond)))
			}
		}
	}
}

func _Socket_ZeroProbing(socket *Socket) {
	timer := time.NewTicker(time.Duration(time.Second * 1))

	oneByte := make([]byte, 1)
	socket.SendBuffer.ReadOneByte(oneByte)
	sentSeq := socket.SND_NXT

	_send_zwp_pkg(socket, oneByte, sentSeq)
	// socket.SND_NXT++

	socket.ZeroProbing = true
	for {
		select {
		case <-timer.C:
			_send_zwp_pkg(socket, oneByte, sentSeq)
		case <-socket.ZeroWNDChan:
			socket.ZeroProbing = false
			return
		}
	}
}

func _Socket_Waiting_ZeroProbing(socket *Socket) {
	/* wait until one byte is written in */
	socket.ZeroProbing = true
	<-socket.ZeroWNDWait

	timer := time.NewTicker(time.Duration(time.Second * 1))

	oneByte := make([]byte, 1)
	socket.SendBuffer.ReadOneByte(oneByte)
	sentSeq := socket.SND_NXT

	_send_zwp_pkg(socket, oneByte, sentSeq)
	// socket.SND_NXT++

	for {
		select {
		case <-timer.C:
			_send_zwp_pkg(socket, oneByte, sentSeq)
		case <-socket.ZeroWNDChan:
			socket.ZeroProbing = false
			return
		}
	}
}
