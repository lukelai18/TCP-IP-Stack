package tcp

import (
	"log"
	"sync"
)

type circularBuffer struct {
	Buffer   []byte
	read     int
	write    int
	size     int
	capacity int
	mutex    sync.Mutex
	notFull  *sync.Cond
	notEmpty *sync.Cond
}

func InitBuffer() *circularBuffer {
	cbuf := &circularBuffer{
		Buffer:   make([]byte, MAX_BUFFER_SZ),
		read:     0,
		write:    0,
		size:     0,
		capacity: MAX_BUFFER_SZ,
	}
	cbuf.notFull = sync.NewCond(&cbuf.mutex)
	cbuf.notEmpty = sync.NewCond(&cbuf.mutex)
	return cbuf
}

func (cbuf *circularBuffer) Put(data []byte) (int, error) {
	cbuf.mutex.Lock()
	defer cbuf.mutex.Unlock()

	dataLen := len(data)
	for cbuf.size+dataLen > cbuf.capacity {
		log.Printf("Put: Not enough space in buffer, waiting...")
		cbuf.notFull.Wait()
	}

	n := dataLen
	firstPart := cbuf.capacity - cbuf.write
	if firstPart >= n {
		copy(cbuf.Buffer[cbuf.write:cbuf.write+n], data)
		cbuf.write = (cbuf.write + n) % cbuf.capacity
	} else {
		copy(cbuf.Buffer[cbuf.write:], data[:firstPart])
		secondPart := n - firstPart
		copy(cbuf.Buffer[0:secondPart], data[firstPart:])
		cbuf.write = secondPart
	}

	cbuf.size += n
	cbuf.notEmpty.Broadcast() // Notify any waiting Get operations
	return n, nil
}

func (cbuf *circularBuffer) Get(buf []byte) (int, error) {
	cbuf.mutex.Lock()
	defer cbuf.mutex.Unlock()

	bufLen := len(buf)
	for cbuf.size < bufLen {
		log.Printf("Get: Not enough data in buffer, waiting...")
		cbuf.notEmpty.Wait()
	}

	n := bufLen
	firstPart := cbuf.capacity - cbuf.read
	if firstPart >= n {
		copy(buf, cbuf.Buffer[cbuf.read:cbuf.read+n])
		cbuf.read = (cbuf.read + n) % cbuf.capacity
	} else {
		copy(buf[:firstPart], cbuf.Buffer[cbuf.read:])
		secondPart := n - firstPart
		copy(buf[firstPart:], cbuf.Buffer[0:secondPart])
		cbuf.read = secondPart
	}

	cbuf.size -= n
	cbuf.notFull.Broadcast() // Notify any waiting Put operations
	return n, nil
}

func (cbuf *circularBuffer) ReadOneByte(data []byte) {
	data[0] = cbuf.Buffer[cbuf.read]
}

func (cbuf *circularBuffer) IsFull() bool {
	return cbuf.read == cbuf.write
}
