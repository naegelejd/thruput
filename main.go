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
	defaultNumConns   = 1
)

var (
	bufsize  int    = 0
	window   int    = 0
	duration int    = 10
	interval int    = 1
	divisor  int    = sizes[defaultFormat]
	unit     string = units[defaultFormat]
)

type IperfConn interface {
	net.Conn
	SetWriteBuffer(bytes int) error
	SetReadBuffer(bytes int) error
}

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
	log.Printf("Using %d threads\n", runtime.NumCPU())

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
	numConns := flag.Int("P", defaultNumConns, "number of parallel clients to run")

	flag.Parse()

	if !*serve && *client == "" ||
		*serve && *client != "" {
		log.Fatal("Must run as *either* server or client")
	}

	var protocol string = "tcp"
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
	type Runnable interface {
		Run() error
	}
	var runnable Runnable
	var err error
	if *serve {
		runnable, err = NewServer(protocol, address)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		runnable, err = NewClient(protocol, address, *numConns)
		if err != nil {
			log.Fatal(err)
		}
	}

	if err = runnable.Run(); err != nil {
		log.Fatal(err)
	}
}

type ConnMaker func() (IperfConn, error)

type Client struct {
	conns []IperfConn
}

func NewClient(protocol, address string, numConns int) (c Client, err error) {
	var maker ConnMaker
	switch protocol {
	case "tcp":
		maker = func() (IperfConn, error) { return makeTCPConn(protocol, address) }
	case "udp":
		maker = func() (IperfConn, error) { return makeUDPConn(protocol, address) }
	default:
		err = fmt.Errorf("unknown protocol")
		return
	}
	for i := 0; i < numConns; i++ {
		var conn IperfConn
		conn, err = maker()
		if err != nil {
			return
		}
		if window > 0 {
			if err = conn.SetWriteBuffer(window); err != nil {
				return
			}
			if err = conn.SetReadBuffer(window); err != nil {
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

type Server interface {
	Run() error
}

func NewServer(protocol, address string) (s Server, err error) {
	switch protocol {

	case "tcp":
		s = TCPServer{protocol, address}
	case "udp":
		s = UDPServer{protocol, address}
	default:
		err = fmt.Errorf("unknown protocol")
	}
	return
}

type TCPServer struct {
	protocol string
	address  string
}

type UDPServer struct {
	protocol string
	address  string
}

func (s TCPServer) Run() error {
	ln, err := net.Listen(s.protocol, s.address)
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		neverTerminate := make(chan bool)
		go handleConn(conn, conn.Read, neverTerminate, nil)
	}
	return nil
}

func (s UDPServer) Run() error {
	udpaddr, err := net.ResolveUDPAddr(s.protocol, s.address)
	if err != nil {
		return err
	}
	for {
		conn, err := net.ListenUDP(s.protocol, udpaddr)
		if err != nil {
			return err
		}
		neverTerminate := make(chan bool)
		go handleConn(conn, conn.Read, neverTerminate, nil)
	}
	return nil
}
