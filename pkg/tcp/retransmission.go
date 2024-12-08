package tcp

import (
	"ipstack_p1/pkg/comm"
	"ipstack_p1/pkg/ip"
	"math"
	"time"
)

func UpdateRTT(socket *Socket, rttSample float64) {
	socket.SRTT = (RTT_ALPHA * socket.SRTT) + ((1 - RTT_ALPHA) * rttSample)
	// Update RTO and clamp between MIN_RTO and MAX_RTO
	socket.ResentRTO = math.Min(MAX_RTO, math.Max(MIN_RTO, socket.SRTT*RTT_BETA))
}

func _Socket_HandlerResentQ(socket *Socket) {
	for {
		/* wait for ack updated */
		socket.ResentQCond.L.Lock()
		socket.ResentQCond.Wait()
		socket.ResentQCond.L.Unlock()

		curtime := time.Now()
		resentRequest := false
		socket.ResentQLock.Lock()

		var size int = 0
		for !socket.ResentQ.IsEmpty() {
			size++
			item := socket.ResentQ.Get()
			if socket.SND_UNA > uint32(item.Priority) {
				/* The package is received */
				/* TODO: Recompute RTO Time */
				if item.Priority+int64(item.Value.payloadSize) > int64(socket.SND_UNA) {
					/* it is not fully received */
					socket.ResentQ.Put(item.Value, item.Priority)
					resentRequest = true
				} else {
					/* it is received */
					continue
				}
				/* RTT update */
				diff := float64(curtime.Sub(item.Value.sentTime).Milliseconds())
				UpdateRTT(socket, diff)

			} else {
				/* The package is not received */
				/* Put the package back to pq and break */
				socket.ResentQ.Put(item.Value, item.Priority)
				resentRequest = true
				break
			}
		}
		// fmt.Printf("Resent Q size is %d\n", size)

		/* if there is no package in the resentQ, stop the RTOTimer */
		if !resentRequest {
			socket.RTOstartRUN = false
			socket.RTOTimer.Stop()
		}
		socket.ResentQLock.Unlock()
	}
}

func _Socket_Retranmission(socket *Socket) {
	/* wait for rtotimer to signal resending */
	for range socket.RTOTimer.C {
		if socket.ResentQ.IsEmpty() {
			continue
		}

		socket.ResentQLock.Lock()
		item := socket.ResentQ.Get()
		socket.ResentQ.Put(item.Value, item.Priority) /* still in the queue */
		socket.ResentQLock.Unlock()

		socket.ResentRTO = min(MAX_RTO, 2*socket.ResentRTO)
		socket.RTOTimer.Reset(time.Duration(socket.ResentRTO * float64(time.Millisecond)))
		ip.Send_TCPPkg_Call(item.Value.srcaddr, item.Value.dstaddr,
			item.Value.sentTcpPkg, int(comm.IpProtoTcp), 32)
	}
}
