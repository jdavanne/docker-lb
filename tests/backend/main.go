package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

var requestCount atomic.Uint64

type Response struct {
	Service      string `json:"service"`
	Hostname     string `json:"hostname"`
	Port         string `json:"port"`
	RequestCount uint64 `json:"request_count"`
	Message      string `json:"message"`
}

var serviceName string

func handler(port string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		hostname, _ := os.Hostname()

		response := Response{
			Service:      serviceName,
			Hostname:     hostname,
			Port:         port,
			RequestCount: count,
			Message:      fmt.Sprintf("Hello from %s:%s:%s (request #%d)", serviceName, hostname, port, count),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

		log.Printf("Request #%d on port %s from %s", count, port, r.RemoteAddr)
	}
}

func main() {
	ports := flag.String("ports", "8081", "Comma-separated list of ports to listen on")
	service := flag.String("service", "unknown", "Service name")
	flag.Parse()

	serviceName = *service
	portList := strings.Split(*ports, ",")
	if len(portList) == 0 {
		log.Fatal("No ports specified")
	}

	hostname, _ := os.Hostname()
	log.Printf("Starting HTTP server on %s, listening on ports: %v", hostname, portList)

	// Start HTTP servers on all specified ports
	for i, port := range portList {
		port = strings.TrimSpace(port)
		if i == len(portList)-1 {
			// Last port - run in foreground
			log.Printf("Listening on port %s (foreground)", port)
			http.HandleFunc("/", handler(port))
			log.Fatal(http.ListenAndServe(":"+port, nil))
		} else {
			// Other ports - run in background
			go func(p string) {
				log.Printf("Listening on port %s (background)", p)
				mux := http.NewServeMux()
				mux.HandleFunc("/", handler(p))
				log.Fatal(http.ListenAndServe(":"+p, mux))
			}(port)
		}
	}
}
