package tcp

import (
	"errors"
	"fmt"
	"time"
)

func _removeSocketFromMaps(socket *Socket) {
	g_TCPStack.sokTableMutex.Lock()
	defer g_TCPStack.sokTableMutex.Unlock()

	// Remove from sidTable
	delete(g_TCPStack.sidTable, socket.tcpSid)

	// Remove from socketTable
	delete(g_TCPStack.socketTable, *socket.sokkey)
}

// Four waves in closing connection
func _Active_Close_Connection(sok *Socket) {
	// Check how much bytes are still needed to be sent, if there are no bytes to be sent
	// continue to close, otherwise, block the close process and wait
	numBytes := _Get_Unfinished(sok)

	sok.SndEmptyCond.L.Lock()
	for numBytes != 0 {
		// fmt.Printf("Start to wait sending over %d\n", numBytes)
		sok.SndEmptyCond.Wait()
		numBytes = _Get_Unfinished(sok)
		// fmt.Printf("numBytes is %d\n", numBytes)
	}
	sok.SndEmptyCond.L.Unlock()

	// Update the latest State
	// (1) Another side receive the closing request, and already send the last part of the data
	//     CLOSE_WAIT → LAST_ACK
	// (2) Initiate side already send the last part of the data and send FIN to the another side
	// 	   ESTABLISHED → FIN_WAIT_1
	if sok.tcpState == CLOSE_WAIT {
		sok.tcpState = LAST_ACK
		// sok.RCV_NXT++
		_send_fin_pkg(sok, []byte{}, sok.RCV_NXT)
	} else {
		sok.tcpState = FIN_WAIT_1
		_send_fin_pkg(sok, []byte{}, sok.RCV_NXT)
	}

	sok.SND_NXT += 1

	// if sok.tcpState == CLOSED {
	// 	fmt.Printf("Socket %d closed\n", sok.tcpSid)
	// }
}

func _doReceive_FIN(socket *Socket, ACK uint32) bool {
	var FIN_FLAG = (socket.SND_NXT == ACK)

	if socket.tcpState == LISTEN || socket.tcpState == SYN_SENT || socket.tcpState == CLOSED {
		return false
	}

	// Wake up the receive buffer condition variable
	socket.RcvBufavailCond.L.Lock()
	socket.RcvBufavailCond.Signal()
	socket.RcvBufavailCond.L.Unlock()
	// Send ACK back

	_send_ackfin_pkg(socket, []byte{}, socket.RCV_NXT+1)
	socket.RCV_NXT += 1

	switch socket.tcpState {
	case ESTABLISHED:
		socket.tcpState = CLOSE_WAIT
	case FIN_WAIT_2:
		socket.tcpState = TIME_WAIT
		go _timeWaitBeforeClose(socket)
	case FIN_WAIT_1:
		// Means that the previous FIN has been
		// acknowledged
		if FIN_FLAG {
			socket.tcpState = TIME_WAIT
			go _timeWaitBeforeClose(socket)
		} else {
			socket.tcpState = CLOSING
		}
	case TIME_WAIT:
		socket.TimeReset <- true
	default:
	}
	return true
}

func _timeWaitBeforeClose(socket *Socket) {
	timer := time.NewTimer(2 * MSL * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			close(socket.tcpPkgChannel)
			socket.tcpState = CLOSED

			// Remove socket from maps
			_removeSocketFromMaps(socket)
			fmt.Printf("Socket %d closed\n", socket.tcpSid)
			return
		case <-socket.TimeReset:
			if !timer.Stop() {
				// Drain the channel if needed
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer.Reset(2 * MSL * time.Second)
	}
}

func VClose(sok *Socket) error {
	// LISTEN and SYN_SENT STATE
	// Havn't start sending data
	if sok.tcpState == LISTEN || sok.tcpState == SYN_SENT {
		close(sok.tcpPkgChannel)
		sok.tcpState = CLOSED
		// Remove socket from maps
		_removeSocketFromMaps(sok)
		fmt.Printf("Socket %d closed\n", sok.tcpSid)
		return nil
	}

	if sok.tcpState == SYN_RECEIVED ||
		sok.tcpState == ESTABLISHED ||
		sok.tcpState == CLOSE_WAIT {
		go _Active_Close_Connection(sok)
		return nil
	}

	return errors.New("ERROR: Cannot close connection")
}
