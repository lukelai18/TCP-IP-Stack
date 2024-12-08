# **TCP/IP Stack Implementation in Go**
# Overview
This project is a custom implementation of a simplified TCP/IP stack in Go. It includes both the IP and TCP layers, providing functionalities such as:

(1) IP Layer: Interface management, routing (including RIP), packet forwarding, and handling. 

(2) TCP Layer: Connection management (three-way handshakes and TCP teardown), data transmission, flow control, retransmissions, and connection termination.
The implementation allows for the establishment of TCP connections, data transfer between hosts, and proper management of connection states and retransmission logic.

# Project Structure
The project is organized into several packages, each responsible for different components of the TCP/IP stack:

main: Entry point of the application. Handles command-line arguments, initializes configurations, and starts the REPL (Read-Eval-Print Loop) for user commands.

comm: Common utilities used across the project.

priority_queue.go: Implements a generic priority queue used for managing retransmission and early arrival packets.
comm.go: Contains common constants and helper functions.
lnxconfig: Handles parsing and loading of configuration files that define interfaces, neighbors, routing modes, and other settings.

ip: Implements the IP layer.

ip.go: Core IP stack initialization and functions.
iphandler.go: Handles incoming IP packets and dispatches them to appropriate handlers based on protocol.
iprouter.go: Implements routing logic, including RIP protocol support.
tcp: Implements the TCP layer.

tcp.go: Core TCP stack initialization and registration of protocol handlers.
socket.go: Defines the Socket and ListenSocket structs and their associated methods.
tcp_interface.go: Handles incoming TCP packets and manages socket state transitions.
tcp_head.go: Functions related to TCP header construction and parsing.
tcp_rw.go: Manages read/write operations and buffer management.
retransmission.go: Implements retransmission logic and timers.
buffer.go: Implements a circular buffer for send and receive buffers.
tcp_close.go: Handles connection termination logic.
tcp_call.go: Provides command-line interface functions for interacting with the TCP stack.


# Key Functionalities
## IP Layer
Interface Management: Initializes and manages network interfaces and neighbors based on configuration files.

Routing:
Static Routing: Supports manually added routes.
Dynamic Routing (RIP): Implements the RIP protocol to exchange routing information with neighboring routers.

Packet Handling:
Packet Forwarding: Forwards IP packets based on the forwarding table.
Packet Reception: Receives IP packets, validates headers, and passes payloads to the appropriate protocol handlers.

## TCP Layer
Connection Management:
Three-Way Handshake: Establishes connections using SYN, SYN-ACK, and ACK packets.
TCP Teardown: Terminates connections gracefully.
State Management: Maintains TCP states (e.g., LISTEN, SYN_SENT, ESTABLISHED).
Data Transmission:
Send/Receive Buffers: Uses circular buffers to store outgoing and incoming data.
Flow Control: Manages the send and receive windows to control data flow.
Retransmission Logic:
Retransmission Queue (ResentQ): Stores unacknowledged segments for potential retransmission.
Timers: Implements RTO (Retransmission Timeout) timers to trigger retransmissions.
RTT Estimation: Updates Round-Trip Time estimates to adjust RTO dynamically.
Concurrency:
Goroutines: Utilizes goroutines to handle concurrent operations such as packet sending, receiving, and retransmission handling.
Synchronization: Employs mutexes and condition variables to synchronize access to shared resources.

# Key Data Structures
Socket Management
Socket Struct: Represents an individual TCP connection.

Connection Identifiers: Contains local and remote IP addresses and ports.
Sequence Numbers:
Send Sequence Variables: SND_UNA (unacknowledged), SND_NXT (next), SND_LBW (last byte written).
Receive Sequence Variables: RCV_NXT (next), RCV_LBR (last byte read).
Windows: SND_WINDOW and RCV_WINDOW for flow control.
Buffers:
SendBuffer: Circular buffer for outgoing data.
RcvBuffer: Circular buffer for incoming data.
Retransmission Queue (ResentQ): Priority queue for managing unacknowledged segments.
Early Arrival Queue (EarlyArrivalQ): Priority queue for handling out-of-order packets.
Synchronization Primitives: Condition variables and mutexes for coordinating goroutines.
ListenSocket Struct: Represents a listening socket awaiting incoming connections.

Listening Port: The port on which the socket is listening.
Connection Queue: Queue of incoming connection requests.
Buffers
circularBuffer Struct: Implements a fixed-size circular buffer.
Buffer Management: Handles read/write indices and size tracking.
Concurrency Support: Mutex and condition variables to manage concurrent access.
Retransmission Management
ResentQ: Priority queue storing segments pending acknowledgment.
Entries: Each entry contains the segment data, sequence number, timestamp, and payload size.
Timers: Associated with RTO timers to trigger retransmissions.
Early Arrival Handling
EarlyArrivalQ: Priority queue for buffering out-of-order segments.
Reassembly Logic: Ensures data is delivered to the application in order.

# Performance Test
Here we try to test the performance for sending and receiving files. Here we use a 1 MB .txt files, 
and set up a 2% drop rate for router. Then compare the performance between reference version and our
version.

Performance of the Reference Implementation

The time needed for sending a 1 MB files is 3.77s(Starting from finishing the three-way handshake process) in reference implementation.

Performance of the Our Implementation

The time needed for sending a 1 MB files is 9.57s(Starting from finishing the three-way handshake process) in reference implementation. 

1. Three-way Handshake
"1","0.000000000","10.0.0.1","10.1.0.2","TCP","82","36011  >  9999 [SYN] Seq=0 Win=65535 Len=0"
"2","0.000692369","10.1.0.2","10.0.0.1","TCP","82","9999  >  36011 [SYN, ACK] Seq=0 Ack=1 Win=65535 Len=0"
"3","0.000905792","10.0.0.1","10.1.0.2","TCP","82","36011  >  9999 [ACK] Seq=1 Ack=1 Win=65535 Len=0"

2. One Segment Sent and Acknowledged
Here we choose the data starting from sequence number 4081.
"7","0.001592169","10.0.0.1","10.1.0.2","TCP","1442","36011  >  9999 [ACK] Seq=4081 Ack=1 Win=65535 Len=1360 [TCP segment of a reassembled PDU]"
"37","0.001999919","10.1.0.2","10.0.0.1","TCP","82","9999  >  36011 [ACK] Seq=1 Ack=5441 Win=60095 Len=0"

3. One Segment That is Retransmitted
Here we choose the data starting from sequence number 36011, which was retransmitted
"124","0.003138601","10.1.0.2","10.0.0.1","TCP","82","[TCP Dup ACK 65#44] 9999  >  36011 [ACK] Seq=1 Ack=21761 Win=65535 Len=0"
"125","0.052543220","10.0.0.1","10.1.0.2","TCP","1442","[TCP Retransmission] 36011  >  9999 [ACK] Seq=21761 Ack=1 Win=65535 Len=1360"

4. Connection teardown
"1558","9.565661794","10.0.0.1","10.1.0.2","TCP","82","36011  >  9999 [FIN, ACK] Seq=1048583 Ack=1 Win=65535 Len=0"
"1559","9.565811187","10.1.0.2","10.0.0.1","TCP","82","9999  >  36011 [ACK] Seq=1 Ack=1048584 Win=65535 Len=0"
"1560","9.565953893","10.1.0.2","10.0.0.1","TCP","82","9999  >  36011 [FIN, ACK] Seq=1 Ack=1048584 Win=65534 Len=0"
"1561","9.566093911","10.0.0.1","10.1.0.2","TCP","82","36011  >  9999 [ACK] Seq=1048584 Ack=2 Win=65535 Len=0"

# Q&A
1. What are the key data structures that represent a connection?
(1) Socket Struct: The central data structure representing a TCP connection.
Sequence Numbers: Manages SND_UNA, SND_NXT, RCV_NXT for tracking sent and received data.
Buffers:
(2) SendBuffer: Stores data to be sent.
(3) RcvBuffer: Stores received data before the application reads it.
(4) Retransmission Queue (ResentQ): A priority queue that holds unacknowledged segments for potential retransmission.
(5) Early Arrival Queue (EarlyArrivalQ): Buffers out-of-order packets until missing segments arrive.
Synchronization Primitives: Condition variables (SndBufavailCond, RcvBufavailCond, etc.) and mutexes for thread coordination.
(6) State Management: Maintains the TCP state (e.g., ESTABLISHED, FIN_WAIT_1).

2. At a high level, how does our TCP logic use threads, and how do they interact with each other?
Goroutines: The TCP logic uses goroutines to handle concurrent operations.
(1) Socket Handler Goroutine (_Socket_HandlePkg):
Responsibility: Processes incoming TCP packets for a socket.
Waits For: Incoming packets on tcpPkgChannel.
Actions: Handles acknowledgments, updates sequence numbers, processes incoming data, manages state transitions (e.g., handling FIN packets).
(2) Send Thread Goroutine (_Socket_SendThread):
Responsibility: Sends data from the SendBuffer when the window allows.
Waits For: Signals on SndreadyCond when new data is available or the send window changes.
Actions: Reads data from SendBuffer, sends segments, updates SND_NXT, and adds segments to ResentQ.
(3) Retransmission Handler Goroutine (_Socket_HandlerResentQ):
Responsibility: Monitors the ResentQ for segments needing retransmission.
Waits For: Signals on ResentQCond when acknowledgments are received or when the RTO timer expires.
Actions: Retransmits unacknowledged segments, updates RTO based on RTT samples.
(4) Retransmission Timer Goroutine (_Socket_Retranmission):
Responsibility: Triggers retransmission events based on the RTO timer.
Waits For: RTO timer expiration events.
Actions: Initiates retransmission of the earliest unacknowledged segment in ResentQ.
Synchronization:

Condition Variables: Used to signal between goroutines when certain events occur (e.g., data becomes available to send).
Mutexes: Protect shared data structures like buffers and queues from concurrent access issues.

3. If we could do this assignment again, what would you change? Any ideas for how we might improve performance?
If I were to redo this assignment, I would consider the following changes and improvements:

(1)Implement Congestion Control:
Add TCP Reno or TCP Tahoe: Implementing congestion control algorithms would make the TCP stack more robust and efficient over networks with variable congestion levels.
(2)Dynamic Buffer Sizes: Allow buffers to resize based on demand to handle varying workloads more efficiently.

4. Any and Possible Bugs:
Error handling is simplified and may cause some bugs.