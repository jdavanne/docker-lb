package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// Version of the software
var Version string

// Build number of the software
var Build string = "dev"

// Date of the build software
var Date string

var (
	probePeriod = flag.Duration("probe-period", 2*time.Second, "Probe period")
	verbose     = flag.Bool("verbose", false, "Verbose mode")
)

var ops atomic.Uint64
var opened atomic.Int64
var cumSent, cumReceived atomic.Int64

func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	fmt.Printf("GoRoutine=%v", runtime.NumGoroutine())
	fmt.Printf("\tAlloc=%v KiB", m.Alloc/1024)
	fmt.Printf("\tTotalAlloc=%v KiB", m.TotalAlloc/1024)
	fmt.Printf("\tSys=%v KiB", m.Sys/1024)
	fmt.Printf("\tNumGC=%v\n", m.NumGC)
}
func checkOption(options []string, name string) (string, bool) {
	for _, option := range options {
		if strings.HasPrefix(option, name+"=") {
			return option[len(name):], true
		} else if option == name {
			return "", true
		}
	}
	return "", false
}

func smain(args []string, clientProxyProtocol, serverProxyProtocol bool, cert, key string) {
	hosts := make(map[string]*DnsProbe)
	for i, arg := range args {
		// fmt.Println(arg)
		options := strings.Split(arg, ",")
		mappings := strings.Split(options[0], ":")
		var porti, host, port string
		if len(mappings) == 3 {
			porti = mappings[0]
			host = mappings[1]
			port = mappings[2]
		} else if len(mappings) == 2 {
			porti = mappings[1]
			host = mappings[0]
			port = mappings[1]
		} else {
			log.Fatal("arg", i, arg, "is not in proti:host:port or host:port format")
		}

		if hosts[host] == nil {
			hosts[host] = NewDnsProbe(host)
			slog.Info("Starting DNS probe", "host", host)
			go hosts[host].dnsProbe()
		}

		_, ok := checkOption(options[1:], "http")
		if ok {
			listenerAndForwardHttp(porti, host, port, clientProxyProtocol, serverProxyProtocol, false, tls.Certificate{}, hosts[host])
		} else if _, ok := checkOption(options[1:], "https"); ok {
			if cert == "" || key == "" {
				//generate self signed key pair
				cert, key = generateSelfSignedCert()
				slog.Info("Self signed certificate generated", "cert", cert, "key", key)
			}
			cer, err := tls.LoadX509KeyPair(cert, key)
			if err != nil {
				log.Fatal(err)
			}
			listenerAndForwardHttp(porti, host, port, clientProxyProtocol, serverProxyProtocol, true, cer, hosts[host])
		} else {
			addr := host + ":" + port
			listenAndForward(porti, addr, clientProxyProtocol, serverProxyProtocol)
		}

	}
	slog.Info("Running...")
}

func main() {
	serverProxyProtocol := flag.Bool("server-proxy-protocol", false, "Enable proxy protocol on server side")
	clientProxyProtocol := flag.Bool("client-proxy-protocol", false, "Enable proxy protocol on client side")
	cert := flag.String("cert", "", "TLS certificate file")
	key := flag.String("key", "", "TLS key file")

	flag.Usage = func() {
		flagSet := flag.CommandLine
		fmt.Printf("Usage of %s: %s\n", os.Args[0], "<(port:)?host:port(,option,...)? ...>")
		flagSet.PrintDefaults()
	}

	flag.Parse()
	slog.Info(os.Args[0], "build", Build, "version", Version, "date", Date)

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	smain(flag.Args(), *clientProxyProtocol, *serverProxyProtocol, *cert, *key)

	c := make(chan int)
	<-c
}
