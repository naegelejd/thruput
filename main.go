package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

const (
	defaultBufsizeTCP = 128000 // 128K
	defaultBufsizeUDP = 8000   // 8K
	defaultPort       = 10101
	defaultClient     = "127.0.0.1"
	defaultFormat     = 'M'
)

var (
	bufsize  int    = 0
	protocol string = "tcp"
	window   int    = 0
	duration int    = 10
	divisor  int    = sizes[defaultFormat]
	unit     string = units[defaultFormat]
)

type Runner interface {
	Run(address string) error
}
type TCPServer struct{}
type UDPServer struct{}
type TCPClient struct{}
type UDPClient struct{}

var sizes = map[rune]int{
	'k': 125, 'm': 1.25e5, 'g': 1.25e8,
	'K': 1e3, 'M': 1e6, 'G': 1e9,
}

var units = map[rune]string{
	'k': "kbps", 'm': "mbps", 'g': "gbps",
	'K': "KB/s", 'M': "MB/s", 'G': "GB/s",
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	serve := flag.Bool("s", false, "run as server")
	client := flag.String("c", "", "connect as client to server (127.0.0.1)")
	udp := flag.Bool("udp", false, "use UDP instead of TCP")
	length := flag.String("len", "", "buffer size (128K for tcp, 8K for udp)")
	flag.IntVar(&window, "window", window, "window size / socket buffer size")
	port := flag.Int("p", defaultPort, "port")
	format := flag.String("f", string(defaultFormat), "[kmgKMG] report format")

	flag.Parse()

	if !*serve && *client == "" ||
		*serve && *client != "" {
		log.Fatal("Must run as *either* server or client")
	}

	if *udp {
		protocol = "udp"
	}

	if len(*length) > 0 {
		var label rune
		_, err := fmt.Sscanf(*length, "%d%c", &bufsize, &label)
		if err != nil {
			log.Fatal(err)
		}
		if bufsize < 0 {
			log.Fatal("buffer size must be > 0")
		}
		bufsize *= sizes[label]
		if bufsize == 0 {
			log.Fatal("invalid unit label")
		}
	} else {
		switch protocol {
		case "tcp":
			bufsize = defaultBufsizeTCP
		case "udp":
			bufsize = defaultBufsizeUDP
		default:
			log.Fatal("unknown protocol")
		}
	}

	label := []rune(*format)[0]
	divisor = sizes[label]
	unit = units[label]
	if divisor == 0 || unit == "" {
		log.Fatal("invalid format")
	}

	address := fmt.Sprintf("%s:%d", *client, *port)
	var thing Runner
	switch protocol {
	case "tcp":
		if *serve {
			thing = TCPServer{}
		} else {
			thing = TCPClient{}
		}
	case "udp":
		if *serve {
			thing = UDPServer{}
		} else {
			thing = UDPClient{}
		}
	default:
		log.Fatal("unknown protocol")
	}
	if err := thing.Run(address); err != nil {
		log.Fatal(err)
	}
}

func (c TCPClient) Run(address string) error {
	conn, err := makeTCPConn(address)
	if err != nil {
		return err
	}
	send(conn)
	return nil
}

func (c UDPClient) Run(address string) error {
	conn, err := makeUDPConn(address)
	if err != nil {
		return err
	}
	send(conn)
	return nil
}

func send(conn net.Conn) {
	defer func() {
		log.Printf("closing connection to server %s\n", conn.RemoteAddr())
		conn.Close()
	}()

	buffer := make([]byte, bufsize)
	limitTimer := time.After(10 * time.Second)
	progressTimer := time.After(1 * time.Second)
	total := 0
	count := 0
loop:
	for {
		select {
		case <-progressTimer:
			log.Printf("count: %d %s\n", count/divisor, unit)
			progressTimer = time.After(1 * time.Second)
			count = 0
		case <-limitTimer:
			break loop
		default:
			n, err := conn.Write(buffer)
			if err != nil {
				log.Println("write error: ", err)
				break loop
			}
			count += n
			total += n
		}
	}
	log.Printf("total: %d %s\n", total/10/divisor, unit)
}

func makeTCPConn(address string) (c net.Conn, err error) {
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
	if window > 0 {
		if err = tcpconn.SetWriteBuffer(window); err != nil {
			return
		}
		if err = tcpconn.SetReadBuffer(window); err != nil {
			return
		}
	}
	return tcpconn, nil
}

func makeUDPConn(address string) (c net.Conn, err error) {
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
	if window > 0 {
		if err = udpconn.SetWriteBuffer(window); err != nil {
			return
		}
		if err = udpconn.SetReadBuffer(window); err != nil {
			return
		}
	}
	return udpconn, nil
}

func (s TCPServer) Run(address string) error {
	ln, err := net.Listen(protocol, address)
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		if err := handleNewClient(conn); err != nil {
			return err
		}
	}
	return nil
}

func (s UDPServer) Run(address string) error {
	udpaddr, err := net.ResolveUDPAddr(protocol, address)
	if err != nil {
		return err
	}
	for {
		conn, err := net.ListenUDP(protocol, udpaddr)
		if err != nil {
			return err
		}
		if err := handleNewClient(conn); err != nil {
			return err
		}
	}
	return nil
}

func handleNewClient(conn net.Conn) error {
	defer func() {
		log.Printf("closing connection to client %s\n", conn.RemoteAddr())
		conn.Close()
	}()
	buffer := make([]byte, bufsize)
	progressTimer := time.After(1 * time.Second)
	total := 0
	count := 0
loop:
	for {
		select {
		case <-progressTimer:
			log.Printf("count: %d %s\n", count/divisor, unit)
			progressTimer = time.After(1 * time.Second)
			count = 0
		default:
			n, err := conn.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Println("read error: ", err)
					return fmt.Errorf("read error: %s", err)
				}
				break loop
			}
			count += n
			total += n
		}
	}
	log.Printf("total: %d %s\n", total/10/divisor, unit)
	return nil
}
