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

func NewClient(protocol, address string, sysBufSize int, numConns int) (c Client, err error) {
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

func autoLabel(bps float64) (rate float64, label string) {
	switch {
	case bps > 1e9:
		rate = bps / 1.25e8
		label = "gbps"
	case bps > 1e6:
		rate = bps / 1.25e5
		label = "mbps"
	case bps > 1e3:
		rate = bps / 125
		label = "kbps"
	default:
		rate = bps
		label = "bps"
	}
	return
}

func report(id int, sentb uint64, period float64, ratebps float64, meanbps float64) {
	var sent float64
	var sentUnit string
	switch {
	case sentb > 1e9:
		sent = float64(sentb) / 1e9
		sentUnit = "GB"
	case sentb > 1e6:
		sent = float64(sentb) / 1e6
		sentUnit = "MB"
	default:
		sent = float64(sentb) / 1e3
		sentUnit = "KB"
	}

	var rate, mean float64
	var rateLabel, meanLabel string
	if unit == autoUnit {
		rate, rateLabel = autoLabel(ratebps)
		mean, meanLabel = autoLabel(meanbps)
	} else {
		rate, rateLabel = ratebps/unit.Divisor, unit.Label
		mean, meanLabel = meanbps/unit.Divisor, unit.Label
	}
	fmt.Printf("[%2d] time: %.02fs, sent: %.02f %s, rate: %.02f %s, mean: %.02f %s\n",
		id, period, sent, sentUnit, rate, rateLabel, mean, meanLabel)
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

	progress := makeIntervalTimer()
	stop := makeStopTimer()
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
				rate := float64(diffBytes) / diffTime
				meanRate := float64(totalBytes) / totalTime
				lastCounts[id] = totalBytes
				lastTimes[id] = now
				report(id, diffBytes, totalTime, rate, meanRate)
			}
			progress = makeIntervalTimer()
		case <-stop:
			break loop
		default:
		}
	}
	for id := 0; id < nconns; id++ {
		count := counts[id]
		diff := time.Now().Sub(startTimes[id]).Seconds()
		report(id, count, diff, float64(count)/diff, float64(count)/diff)
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
