package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"runtime"
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
	bufsize    int    = 0
	protocol   string = "tcp"
	window     int    = 0
	duration   int    = 10
	interval   int    = 1
	divisor    int    = sizes[defaultFormat]
	unit       string = units[defaultFormat]
	numClients int    = 1
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
	runtime.GOMAXPROCS(runtime.NumCPU())

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	serve := flag.Bool("s", false, "run as server")
	client := flag.String("c", "", "connect as client to server (127.0.0.1)")
	udp := flag.Bool("udp", false, "use UDP instead of TCP")
	length := flag.String("len", "", "buffer size (128K for tcp, 8K for udp)")
	port := flag.Int("p", defaultPort, "port")
	format := flag.String("f", string(defaultFormat), "[kmgKMG] report format")
	flag.IntVar(&window, "window", window, "window size / socket buffer size")
	flag.IntVar(&duration, "t", duration, "duration in seconds")
	flag.IntVar(&interval, "i", interval, "interval in seconds")
	flag.IntVar(&numClients, "P", numClients, "number of parallel clients to run")

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

type ConnMaker func() (net.Conn, error)

func (c TCPClient) Run(address string) error {
	return run(func() (net.Conn, error) {
		return makeTCPConn(address)
	})
}

func (c UDPClient) Run(address string) error {
	return run(func() (net.Conn, error) {
		return makeUDPConn(address)
	})
}

func run(maker ConnMaker) error {
	for i := 0; i < numClients; i++ {
		conn, err := maker()
		if err != nil {
			return err
		}
		handleConn(i, conn, conn.Write, makeTimeout())
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

func handleConn(id int, conn net.Conn, action Action, terminator <-chan bool) {
	defer func() {
		log.Printf("closing connection to %s\n", conn.RemoteAddr())
		conn.Close()
	}()

	buffer := make([]byte, bufsize)
	// limitTimer := time.After(time.Duration(duration) * time.Second)
	progressTimer := time.After(time.Duration(interval) * time.Second)
	total := 0
	count := 0
loop:
	for {
		select {
		case <-progressTimer:
			fmt.Printf("[%02d] count: %d %s\n", id, count/interval/divisor, unit)
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
			count += n
			total += n
		}
	}
	fmt.Printf("[%02d] total: %d %s\n", id, total/duration/divisor, unit)
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
		neverTerminate := make(chan bool)
		handleConn(0, conn, conn.Read, neverTerminate)
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
		neverTerminate := make(chan bool)
		handleConn(0, conn, conn.Read, neverTerminate)
	}
	return nil
}
