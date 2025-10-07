package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strconv"
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

// parsePortRange parses a port string which can be a single port or a range (port1-port2)
// Returns a slice of port strings
func parsePortRange(portStr string) ([]string, error) {
	if !strings.Contains(portStr, "-") {
		// Single port
		return []string{portStr}, nil
	}

	// Port range
	parts := strings.SplitN(portStr, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid port range format: %s", portStr)
	}

	port1, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid start port in range %s: %v", portStr, err)
	}

	port2, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid end port in range %s: %v", portStr, err)
	}

	if port1 > port2 {
		return nil, fmt.Errorf("invalid port range %s: start port must be <= end port", portStr)
	}

	// Expand range
	var ports []string
	for port := port1; port <= port2; port++ {
		ports = append(ports, strconv.Itoa(port))
	}

	return ports, nil
}

func smain(args []string, clientProxyProtocol, serverProxyProtocol bool, cert, key string) {
	hosts := make(map[string]*DnsProbe)
	for i, arg := range args {
		// fmt.Println(arg)
		options := strings.Split(arg, ",")
		mappings := strings.Split(options[0], ":")
		var portiStr, host, portStr string
		if len(mappings) == 3 {
			portiStr = mappings[0]
			host = mappings[1]
			portStr = mappings[2]
		} else if len(mappings) == 2 {
			portiStr = mappings[1]
			host = mappings[0]
			portStr = mappings[1]
		} else {
			log.Fatal("arg", i, arg, "is not in proti:host:port or host:port format")
		}

		// Parse port ranges
		listenPorts, err := parsePortRange(portiStr)
		if err != nil {
			log.Fatalf("arg %d: error parsing listen port range: %v", i, err)
		}

		backendPorts, err := parsePortRange(portStr)
		if err != nil {
			log.Fatalf("arg %d: error parsing backend port range: %v", i, err)
		}

		// Validate that ranges have the same length
		if len(listenPorts) != len(backendPorts) {
			log.Fatalf("arg %d: listen port range (%d ports) and backend port range (%d ports) must have the same length",
				i, len(listenPorts), len(backendPorts))
		}

		if hosts[host] == nil {
			hosts[host] = NewDnsProbe(host)
			slog.Info("Starting DNS probe", "host", host)
			go hosts[host].dnsProbe()
		}

		// Create service for each port in the range
		for j := range len(listenPorts) {
			porti := listenPorts[j]
			port := backendPorts[j]

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
		fmt.Printf("Usage of %s: %s\n", os.Args[0], "<(port[-port2]:)?host:port[-port2](,option,...)? ...>")
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
