package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

type IperfConn interface {
	net.Conn
	SetWriteBuffer(bytes int) error
	SetReadBuffer(bytes int) error
}

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

func writeForever(conn IperfConn, byteCount *uint64) {
	buffer := make([]byte, bufsize)
	for {
		n, err := conn.Write(buffer)
		if err != nil {
			if err != io.EOF {
				log.Println("IO error: ", err)
			}
		}
		*byteCount += uint64(n)
	}
}

func (c Client) Run() error {
	nconns := len(c.conns)

	counts := make([]uint64, nconns)
	lastCounts := make([]uint64, nconns)
	startTimes := make([]time.Time, nconns)
	lastTimes := make([]time.Time, nconns)

	for id, conn := range c.conns {
		go writeForever(conn, &counts[id])
		now := time.Now()
		startTimes[id] = now
		lastTimes[id] = now
	}

	progress := time.After(time.Duration(interval) * time.Second)
	stop := time.After(time.Duration(duration) * time.Second)
loop:
	for {
		select {
		case <-progress:
			for id := 0; id < nconns; id++ {
				now := time.Now()
				totalBytes := counts[id]
				diffBytes := totalBytes - lastCounts[id]
				totalTime := now.Sub(startTimes[id]).Seconds()
				diffTime := now.Sub(lastTimes[id]).Seconds()
				rate := float64(diffBytes) / diffTime / float64(divisor)
				meanRate := float64(totalBytes) / totalTime / float64(divisor)
				fmt.Printf("[%02d] sent: %.02f, time: %.02fs, rate: %.02f %s, mean: %.02f %s\n",
					id, float64(diffBytes)/float64(divisor), totalTime, rate, unit, meanRate, unit)
				lastCounts[id] = totalBytes
				lastTimes[id] = now
			}
			progress = time.After(time.Duration(interval) * time.Second)
		case <-stop:
			break loop
		default:
		}
	}
	for id := 0; id < nconns; id++ {
		count := counts[id]
		diff := time.Now().Sub(startTimes[id]).Seconds()
		fmt.Printf("[%02d] total: %.02f, time: %.02fs, mean: %.02f %s\n",
			id, float64(count)/float64(divisor), diff,
			float64(count)/float64(diff)/float64(divisor), unit)
	}

	return nil
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
