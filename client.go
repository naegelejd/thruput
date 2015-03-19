package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

type ConnMaker func() (IperfConn, error)

type Client struct {
	conns []IperfConn
}

func NewClient(protocol, address string, numConns int) (c Client, err error) {
	for i := 0; i < numConns; i++ {
		var conn IperfConn
		switch protocol {
		case "tcp":
			conn, err = makeTCPConn(protocol, address)
		case "udp":
			conn, err = makeUDPConn(protocol, address)
		default:
			err = fmt.Errorf("unknown protocol")
			return
		}
		if err != nil {
			return
		}

		if sysBufSize > 0 {
			if err = conn.SetWriteBuffer(sysBufSize); err != nil {
				return
			}
			if err = conn.SetReadBuffer(sysBufSize); err != nil {
				return
			}
		}
		c.conns = append(c.conns, conn)
	}
	return
}

func (c Client) Run() error {
	nconns := len(c.conns)
	allResults := make([]chan int64, nconns)
	for i, conn := range c.conns {
		results := make(chan int64) // unbuffered
		allResults[i] = results
		go handleConn(conn, conn.Write, makeTimeout(), results)
	}

	// loop as long as we have results channels to read from
	totals := make([]int64, nconns)
	numClosed := 0
	for numClosed < nconns {
		for id, results := range allResults {
			if results == nil {
				continue
			}
			count, ok := <-results
			if !ok {
				// channel is closed so mark it nil
				allResults[id] = nil
				numClosed++
				continue
			}
			totals[id] += count
			fmt.Printf("[%02d] count: %d, time: %ds, rate: %.02f %s\n", id,
				count/int64(divisor), interval,
				float64(count)/float64(interval)/float64(divisor), unit)
		}
	}
	for id, total := range totals {
		fmt.Printf("[%02d] count: %d, time: %ds, mean: %.02f %s\n", id,
			total/int64(divisor), duration,
			float64(total)/float64(duration)/float64(divisor), unit)
	}
	return nil
}

func makeTimeout() <-chan bool {
	done := make(chan bool)
	go func() {
		<-time.After(time.Duration(duration) * time.Second)
		done <- true
	}()
	return done
}

// type Action abstracts net.Conn.Read and net.Conn.Write
type Action func([]byte) (int, error)

func handleConn(conn net.Conn, action Action, terminator <-chan bool, results chan<- int64) {
	// log.Printf("handling connection %d (%s)\n", id, conn.LocalAddr())
	defer func() {
		// log.Printf("closing connection to %s\n", conn.RemoteAddr())
		conn.Close()
	}()

	buffer := make([]byte, bufsize)
	progressTimer := time.After(time.Duration(interval) * time.Second)
	var count int64
loop:
	for {
		select {
		case <-progressTimer:
			if results != nil {
				results <- count
			}
			progressTimer = time.After(time.Duration(interval) * time.Second)
			count = 0
		case <-terminator:
			break loop
		default:
			n, err := action(buffer)
			if err != nil {
				if err != io.EOF {
					log.Println("IO error: ", err)
				}
				break loop
			}
			count += int64(n)
		}
	}
	if results != nil {
		close(results)
	}
}

func makeTCPConn(protocol, address string) (c IperfConn, err error) {
	var tcpaddr *net.TCPAddr
	var tcpconn *net.TCPConn

	tcpaddr, err = net.ResolveTCPAddr(protocol, address)
	if err != nil {
		return
	}
	tcpconn, err = net.DialTCP(protocol, nil, tcpaddr)
	if err != nil {
		return
	}

	return tcpconn, nil
}

func makeUDPConn(protocol, address string) (c IperfConn, err error) {
	var udpaddr *net.UDPAddr
	var udpconn *net.UDPConn

	udpaddr, err = net.ResolveUDPAddr(protocol, address)
	if err != nil {
		return
	}
	udpconn, err = net.DialUDP(protocol, nil, udpaddr)
	if err != nil {
		return
	}
	return udpconn, nil
}
