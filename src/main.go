package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

var (
	probePeriod = flag.Duration("probe-period", 2*time.Second, "Probe period")
	verbose     = flag.Bool("verbose", false, "Verbose mode")
)

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

func dnsProbe(host string) {
	slog.Info("Resolving", "host", host)
	m := make(map[string]int)
	round := 0
	for {
		time.Sleep(*probePeriod)
		round++

		if *verbose {
			PrintMemUsage()
			slog.Info("Probing...", "host", host)
		}

		ips, err := net.LookupIP(host)
		if err != nil {
			slog.Error("Lookup failed", "host", host, "err", err)
			continue
		}

		for _, ip := range ips {
			if m[ip.String()] == 0 {
				slog.Info("New", "host", host, "ip", ip.String())
			}
			m[ip.String()] = round
		}

		for k, v := range m {
			if v != round {
				slog.Info("Lost", "host", host, "ip", k)
				delete(m, k)
			}
		}
	}
}

func forward(c net.Conn, addr string, port string) {
	defer c.Close()

	// Connect to the remote server
	remote, err := net.Dial("tcp", addr)
	if err != nil {
		slog.Error("Dial failed", "addr", addr, "err", err)
		return
	}
	defer remote.Close()

	slog.Info("Forwarding", "port", port, "remote", remote.RemoteAddr())

	var closed atomic.Bool
	// Run in parallel to prevent blocking
	go func() {
		defer c.Close()
		// Copy the data from the client to the remote server
		_, err := io.Copy(remote, c)
		if err != nil && closed.Load() == false {
			slog.Error("Connection error", "remote", remote.RemoteAddr(), "addr", addr, "err", err)
		}
		closed.Store(true)
	}()

	// Copy the data from the remote server to the client
	_, err = io.Copy(c, remote)
	if err != nil && closed.Load() == false {
		slog.Error("Connection error", "remote", remote.RemoteAddr(), "addr", addr, "err", err)
	}
	closed.Store(true)
}

func listenAndForward(port string, addr string) {
	slog.Info("Forwarding", "port", port, "addr", addr)

	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		defer l.Close()
		for {
			// Wait for a connection.
			conn, err := l.Accept()
			if err != nil {
				log.Fatal(err)
			}
			// Handle the connection in a new goroutine.
			// The loop then returns to accepting, so that
			// multiple connections may be served concurrently.
			go forward(conn, addr, port)
		}
	}()
}

func smain(args []string) {
	hosts := make(map[string]bool)
	for i, arg := range args {
		// fmt.Println(arg)
		mappings := strings.Split(arg, ":")
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

		addr := host + ":" + port
		listenAndForward(porti, addr)
		if !hosts[host] {
			hosts[host] = true
			slog.Info("Starting DNS probe", "host", host)
			go dnsProbe(host)
		}
	}
	slog.Info("Running...")
}

func main() {
	flag.Usage = func() {
		flagSet := flag.CommandLine
		fmt.Printf("Usage of %s: %s\n", os.Args[0], "<port:host:port...>")
		flagSet.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	smain(flag.Args())

	c := make(chan int)
	<-c
}
