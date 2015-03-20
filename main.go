package main

import (
	"fmt"
	"log"
	"os"
	"runtime"

	"gopkg.in/alecthomas/kingpin.v1"
)

const (
	defaultBufsizeTCP = 128000 // 128K
	defaultBufsizeUDP = 8000   // 8K
	defaultFormat     = 'M'
)

var (
	bufsize    int
	sysBufSize int
	duration   int
	interval   int
	divisor    int    = sizes[defaultFormat]
	unit       string = units[defaultFormat]
)

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

	app := kingpin.New("iperf", "a tcp/udp network throughput measurement tool")
	// debug := app.Flag("debug", "enable debug mode").Bool()
	port := app.Flag("port", "specify port").Short('p').Default("10101").Int()
	udp := app.Flag("udp", "use UDP instead of TCP").Short('u').Bool()
	length := app.Flag("len", "application buffer size (128K for tcp, 8K for udp)").Short('l').String()
	clientCmd := app.Command("client", "run as client")
	host := clientCmd.Arg("host", "host to connect to").Required().String()
	numConns := clientCmd.Flag("num", "number of concurrent clients").Short('P').Default("1").Int()
	format := clientCmd.Flag("fmt", "report format").Short('f').PlaceHolder("[kmgKMG]").Default(string(defaultFormat)).String()
	clientCmd.Flag("duration", "duration in seconds").Short('t').Default("10").IntVar(&duration)
	clientCmd.Flag("interval", "interval in seconds").Short('i').Default("1").IntVar(&interval)
	clientCmd.Flag("sys", "OS socket buffer size").IntVar(&sysBufSize)
	serverCmd := app.Command("server", "run as server")

	parsed := kingpin.MustParse(app.Parse(os.Args[1:]))

	fmt.Println(duration, interval)

	var protocol = "tcp"
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

	address := fmt.Sprintf("%s:%d", *host, *port)

	type Runnable interface {
		Run() error
	}
	var runnable Runnable
	var err error
	switch {
	case parsed == clientCmd.FullCommand():
		label := []rune(*format)[0]
		divisor = sizes[label]
		unit = units[label]
		if divisor == 0 || unit == "" {
			log.Fatal("invalid format")
		}
		runnable, err = NewClient(protocol, address, *numConns)
		if err != nil {
			log.Fatal(err)
		}
	case parsed == serverCmd.FullCommand():
		runnable, err = NewServer(protocol, address)
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("Must run as *either* server or client")
	}

	if err = runnable.Run(); err != nil {
		log.Fatal(err)
	}
}
