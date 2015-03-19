package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"runtime"
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
	bufsize    int    = 0
	sysBufSize int    = 0
	duration   int    = 10
	interval   int    = 1
	divisor    int    = sizes[defaultFormat]
	unit       string = units[defaultFormat]
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

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	serve := flag.Bool("s", false, "run as server")
	client := flag.String("c", "", "connect as client to server (127.0.0.1)")
	udp := flag.Bool("udp", false, "use UDP instead of TCP")
	length := flag.String("len", "", "application buffer size (128K for tcp, 8K for udp)")
	port := flag.Int("p", defaultPort, "port")
	format := flag.String("f", string(defaultFormat), "[kmgKMG] report format")
	flag.IntVar(&sysBufSize, "sys", sysBufSize, "OS socket buffer size")
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
