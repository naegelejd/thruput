package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"gopkg.in/alecthomas/kingpin.v1"
)

const (
	defaultBufsizeTCP = 128000 // 128K
	defaultBufsizeUDP = 8000   // 8K
)

var (
	bufsize           int
	unit              *Unit
	makeStopTimer     func() <-chan time.Time
	makeIntervalTimer func() <-chan time.Time
)

const (
	unitAuto = 'A'
	unitKbps = 'k'
	unitMbps = 'm'
	unitGbps = 'g'
	unitKB_s = 'K'
	unitMB_s = 'M'
	unitGB_s = 'G'
)

var unitMultipliers = map[rune]int{
	unitKB_s: 1024, unitMB_s: 1024 * 1024, unitGB_s: 1024 * 1024 * 1024,
}

type Unit struct {
	Divisor float64
	Label   string
}

var autoUnit = &Unit{}
var units = map[rune]*Unit{
	unitAuto: autoUnit,
	unitKbps: &Unit{125, "kbps"}, unitMbps: &Unit{1.25e5, "mbps"},
	unitGbps: &Unit{1.25e8, "gbps"}, unitKB_s: &Unit{1e3, "KB/s"},
	unitMB_s: &Unit{1e6, "MB/s"}, unitGB_s: &Unit{1e9, "GB/s"},
}

func allUnits() []string {
	return []string{string(unitAuto),
		string(unitKbps), string(unitMbps), string(unitGbps),
		string(unitKB_s), string(unitMB_s), string(unitGB_s),
	}
}

func die(msg string) {
	fmt.Println("error:", msg)
	os.Exit(1)
}

func makeTimer(seconds float64) func() <-chan time.Time {
	if seconds <= 0 {
		return func() <-chan time.Time {
			return nil
		}
	} else {
		return func() <-chan time.Time {
			return time.After(time.Duration(seconds * float64(time.Second)))
		}
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	app := kingpin.New("thruput", "a tcp/udp network throughput measurement tool")
	port := app.Flag("port", "specify port").Short('p').Default("10101").Int()
	udp := app.Flag("udp", "use UDP instead of TCP").Short('u').Bool()
	length := app.Flag("len", "application buffer size (128K for tcp, 8K for udp)").Short('l').PlaceHolder("#[KMG]").String()
	clientCmd := app.Command("client", "run as client")
	host := clientCmd.Arg("host", "host to connect to").Required().String()
	numConns := clientCmd.Flag("nclients", "number of concurrent clients").Short('P').Default("1").Int()
	format := clientCmd.Flag("format", "transfer rate units [A=auto]").Short('f').Default(string(unitAuto)).Enum(allUnits()...)
	duration := clientCmd.Flag("duration", "duration in seconds [0=forever]").Short('t').Default("10").Float()
	interval := clientCmd.Flag("interval", "interval in seconds [0=none]").Short('i').Default("1").Float()
	sysBufSize := clientCmd.Flag("sys", "OS socket buffer size").Int()
	serverCmd := app.Command("server", "run as server")

	parsed := kingpin.MustParse(app.Parse(os.Args[1:]))

	var protocol = "tcp"
	bufsize = defaultBufsizeTCP
	if *udp {
		protocol = "udp"
		bufsize = defaultBufsizeUDP
	}

	if len(*length) > 0 {
		var label rune
		_, err := fmt.Sscanf(*length, "%d%c", &bufsize, &label)
		if err != nil {
			log.Fatal(err)
		}
		if bufsize < 0 {
			die("buffer size must be > 0")
		}
		bufsize *= unitMultipliers[label]
		if bufsize == 0 {
			die("invalid unit label")
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
		u := []rune(*format)[0]
		unit = units[u]
		if unit == nil {
			die("invalid format")
		}
		if *numConns < 1 {
			die("nclients must be > 0")
		}
		makeIntervalTimer = makeTimer(*interval)
		makeStopTimer = makeTimer(*duration)
		runnable, err = NewClient(protocol, address, *sysBufSize, *numConns)
		if err != nil {
			log.Fatal(err)
		}
	case parsed == serverCmd.FullCommand():
		runnable, err = NewServer(protocol, address)
		if err != nil {
			log.Fatal(err)
		}
	default:
		die("Must run as *either* server or client")
	}

	if err = runnable.Run(); err != nil {
		log.Fatal(err)
	}
}
