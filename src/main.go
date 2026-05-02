package main

import (
	"crypto/tls"
	"flag"
	"fmt"
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

// ProxyProtocolConfig holds proxy protocol configuration
type ProxyProtocolConfig struct {
	ServerEnabled bool
	ServerVersion byte // 1 or 2
	ClientEnabled bool
	ClientVersion byte // 1 or 2
}

var (
	probePeriod      = flag.Duration("probe-period", 2*time.Second, "Probe period")
	verbose          = flag.Bool("verbose", false, "Verbose mode")
	lbAlgorithm      = flag.String("lb-algorithm", "random", "Load balancing algorithm: random, round-robin, least-connection, weighted-random")
	affinityTTL      = flag.Duration("affinity-ttl", 30*time.Second, "IP affinity TTL (0 to disable)")
	backendWeightsFlag = flag.String("backend-weights", "", "Backend weights: host:ip1=weight1,ip2=weight2,...")
	statsPort        = flag.String("stats-port", "8080", "Stats/management API port")
)

var ops atomic.Uint64
var opened atomic.Int64
var cumSent, cumReceived atomic.Int64

func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	slog.Info("Memory usage",
		"goroutines", runtime.NumGoroutine(),
		"alloc_kib", m.Alloc/1024,
		"total_alloc_kib", m.TotalAlloc/1024,
		"sys_kib", m.Sys/1024,
		"num_gc", m.NumGC,
	)
}
func checkOption(options []string, name string) (string, bool) {
	for _, option := range options {
		if strings.HasPrefix(option, name+"=") {
			return option[len(name)+1:], true
		} else if option == name {
			return "", true
		}
	}
	return "", false
}

// parseBackendWeights parses backend weights from CLI flag
// Format: host:ip1=weight1,ip2=weight2;host2:ip3=weight3,...
func parseBackendWeights(weightStr string) map[string]map[string]int {
	result := make(map[string]map[string]int)
	if weightStr == "" {
		return result
	}

	// Split by semicolon for different hosts
	hostEntries := strings.Split(weightStr, ";")
	for _, entry := range hostEntries {
		if entry == "" {
			continue
		}

		// Split by colon to get host and weights
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			slog.Warn("Invalid backend weight entry", "entry", entry)
			continue
		}

		host := parts[0]
		weightsStr := parts[1]

		// Parse individual IP weights
		weights := make(map[string]int)
		ipWeights := strings.Split(weightsStr, ",")
		for _, ipWeight := range ipWeights {
			ipWeightParts := strings.SplitN(ipWeight, "=", 2)
			if len(ipWeightParts) != 2 {
				slog.Warn("Invalid IP weight", "entry", ipWeight)
				continue
			}

			ip := ipWeightParts[0]
			weight, err := strconv.Atoi(ipWeightParts[1])
			if err != nil {
				slog.Warn("Invalid weight value", "ip", ip, "weight", ipWeightParts[1])
				continue
			}

			weights[ip] = weight
		}

		if len(weights) > 0 {
			result[host] = weights
		}
	}

	return result
}

// parseProxyProtocolOption parses proxy protocol option value (v1, v2, or empty)
// Returns (enabled, version, error)
func parseProxyProtocolOption(value string) (bool, byte, error) {
	if value == "" {
		return false, 0, nil
	}
	switch value {
	case "v1", "1":
		return true, 1, nil
	case "v2", "2":
		return true, 2, nil
	default:
		return false, 0, fmt.Errorf("invalid proxy protocol version: %s (must be v1 or v2)", value)
	}
}

// parseProxyProtocolConfig parses proxy protocol options from command-line options
// Supports: proxy-server=v1|v2, proxy-client=v1|v2
func parseProxyProtocolConfig(options []string, globalClient, globalServer bool) (ProxyProtocolConfig, error) {
	config := ProxyProtocolConfig{}

	// Check for per-mapping options first
	serverOpt, hasServer := checkOption(options, "proxy-server")
	clientOpt, hasClient := checkOption(options, "proxy-client")

	if hasServer {
		enabled, version, err := parseProxyProtocolOption(serverOpt)
		if err != nil {
			return config, err
		}
		config.ServerEnabled = enabled
		config.ServerVersion = version
	} else if globalServer {
		// Fallback to global flag (backward compatibility)
		config.ServerEnabled = true
		config.ServerVersion = 1 // Default to v1 for backward compat
	}

	if hasClient {
		enabled, version, err := parseProxyProtocolOption(clientOpt)
		if err != nil {
			return config, err
		}
		config.ClientEnabled = enabled
		config.ClientVersion = version
	} else if globalClient {
		// Fallback to global flag (backward compatibility)
		config.ClientEnabled = true
		config.ClientVersion = 1 // Default to v1 for backward compat
	}

	return config, nil
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

// backendTarget represents a parsed host:port backend target
type backendTarget struct {
	host string
	port string
}

// parseTargets parses the target portion of a mapping (after the listen port).
// Supports:
//   - "host:port" — single target
//   - "host:port1+port2+port3" — same host, multiple ports
//   - "host1:port1+host2:port2" — multiple host:port targets
func parseTargets(targetStr string) ([]backendTarget, error) {
	segments := strings.Split(targetStr, "+")
	var targets []backendTarget

	for _, seg := range segments {
		parts := strings.SplitN(seg, ":", 2)
		if len(parts) == 2 {
			// host:port or host:port-range
			targets = append(targets, backendTarget{host: parts[0], port: parts[1]})
		} else if len(parts) == 1 {
			// Just a port (or port range) — inherit host from first target
			if len(targets) == 0 {
				return nil, fmt.Errorf("port-only target %q must follow a host:port target", seg)
			}
			targets = append(targets, backendTarget{host: targets[0].host, port: parts[0]})
		} else {
			return nil, fmt.Errorf("invalid target segment: %q", seg)
		}
	}
	return targets, nil
}

func smain(args []string, clientProxyProtocol, serverProxyProtocol bool, cert, key string) {
	// Parse backend weights
	backendWeights := parseBackendWeights(*backendWeightsFlag)

	// Track DNS resolvers per host (not host:port)
	resolvers := make(map[string]*DNSResolver)

	// Track backend pools and affinity maps per host
	pools := make(map[string]*BackendPool)
	affinityMaps := make(map[string]*AffinityMap)

	// Create and start stats server
	statsServer := NewStatsServer()
	if *statsPort != "" {
		statsServer.Start(":" + *statsPort)
	}

	// Start memory monitoring goroutine if verbose
	if *verbose {
		go func() {
			for {
				time.Sleep(*probePeriod)
				PrintMemUsage()
			}
		}()
	}

	for i, arg := range args {
		options := strings.Split(arg, ",")
		mapping := options[0]

		// Parse listen_port:targets format
		// First colon-separated segment that looks like a port (or port range) is the listen port
		// Rest is the target string (which may contain colons due to host:port pairs)
		var portiStr, targetStr string
		colonIdx := strings.Index(mapping, ":")
		if colonIdx == -1 {
			slog.Error("Invalid argument format", "arg", i, "value", arg, "expected", "porti:host:port or host:port")
			os.Exit(1)
		}

		firstPart := mapping[:colonIdx]
		rest := mapping[colonIdx+1:]

		// Determine if firstPart is a listen port (numeric or range) or a hostname
		_, portErr := parsePortRange(firstPart)
		// Check if rest contains another colon (meaning first part is listen port, rest is host:port)
		if portErr == nil && strings.Contains(rest, ":") {
			// listen_port:host:port(+...) format
			portiStr = firstPart
			targetStr = rest
		} else {
			// host:port format (listen port = backend port)
			portiStr = rest
			targetStr = mapping
		}

		// Parse listen port range
		listenPorts, err := parsePortRange(portiStr)
		if err != nil {
			slog.Error("Error parsing listen port range", "arg", i, "err", err)
			os.Exit(1)
		}

		// Parse targets (supports + for multi-target)
		targets, err := parseTargets(targetStr)
		if err != nil {
			slog.Error("Error parsing targets", "arg", i, "err", err)
			os.Exit(1)
		}

		// Parse options
		_, httpMode := checkOption(options[1:], "http")
		_, httpsMode := checkOption(options[1:], "https")
		_, affinityEnabled := checkOption(options[1:], "affinity")
		algorithmOpt, hasAlgorithm := checkOption(options[1:], "lb")

		// Determine algorithm (port-specific or global default)
		algorithm := *lbAlgorithm
		if hasAlgorithm {
			algorithm = algorithmOpt
		}

		// Parse proxy protocol configuration
		proxyConfig, err := parseProxyProtocolConfig(options[1:], clientProxyProtocol, serverProxyProtocol)
		if err != nil {
			slog.Error("Error parsing proxy protocol config", "arg", i, "err", err)
			os.Exit(1)
		}

		// Expand all targets with port ranges
		type expandedTarget struct {
			host string
			port string
		}
		var allTargets []expandedTarget
		for _, t := range targets {
			ports, err := parsePortRange(t.port)
			if err != nil {
				slog.Error("Error parsing target port range", "arg", i, "host", t.host, "port", t.port, "err", err)
				os.Exit(1)
			}
			for _, p := range ports {
				allTargets = append(allTargets, expandedTarget{host: t.host, port: p})
			}
		}

		// For single-target with port ranges, validate against listen ports
		if len(targets) == 1 {
			backendPorts, _ := parsePortRange(targets[0].port)
			if len(listenPorts) != len(backendPorts) {
				if len(backendPorts) == 1 {
					// Expand single backend port to match listen range
					singlePort := backendPorts[0]
					backendPorts = make([]string, len(listenPorts))
					for j := range backendPorts {
						backendPorts[j] = singlePort
					}
				} else {
					slog.Error("Port range mismatch", "arg", i, "listenPorts", len(listenPorts), "backendPorts", len(backendPorts), "hint", "backend must be single port or same range length")
					os.Exit(1)
				}
			}
			// Rebuild allTargets from validated ports
			allTargets = nil
			for _, p := range backendPorts {
				allTargets = append(allTargets, expandedTarget{host: targets[0].host, port: p})
			}
		}

		// Ensure DNS resolvers exist for all target hosts
		for _, t := range allTargets {
			if resolvers[t.host] == nil {
				resolvers[t.host] = NewDNSResolver(t.host, *probePeriod)
				go resolvers[t.host].start()
				slog.Info("Starting DNS resolver", "host", t.host, "probePeriod", *probePeriod)
			}
		}

		// Multi-target mode: multiple targets specified with +
		isMultiTarget := len(targets) > 1

		// For multi-target: all targets share a single listen port
		if isMultiTarget && len(listenPorts) > 1 {
			slog.Error("Multi-target (+) cannot be combined with listen port ranges", "arg", i)
			os.Exit(1)
		}

		if isMultiTarget {
			// Multi-target mode: create pools for all targets and combine into a MultiPool
			porti := listenPorts[0]
			var subPools []*BackendPool

			for _, t := range allTargets {
				poolKey := t.host + ":" + t.port
				if pools[poolKey] == nil {
					pools[poolKey] = NewBackendPool(t.host, t.port)
					resolvers[t.host].Subscribe(pools[poolKey])
					slog.Info("Backend pool subscribed to DNS resolver", "host", t.host, "port", t.port)

					if weights, ok := backendWeights[t.host]; ok {
						pools[poolKey].SetWeights(weights)
					}
					statsServer.RegisterBackendPool(poolKey, pools[poolKey])
				}
				subPools = append(subPools, pools[poolKey])
			}

			// Create multi-pool or use single pool
			var pool Pool
			if len(subPools) == 1 {
				pool = subPools[0]
			} else {
				pool = NewMultiPool(subPools)
			}

			// Create affinity map if enabled
			var affinity *AffinityMap
			if affinityEnabled {
				affinityKey := porti
				if affinityMaps[affinityKey] == nil {
					ttl := *affinityTTL
					if ttl == 0 {
						ttl = 30 * time.Second
					}
					affinityMaps[affinityKey] = NewAffinityMap(affinityKey, ttl)
					slog.Info("IP affinity enabled", "listenPort", porti, "ttl", ttl)
					statsServer.RegisterAffinityMap(affinityKey, affinityMaps[affinityKey])
				}
				affinity = affinityMaps[affinityKey]
			}

			hasExplicitWeights := false
			for _, t := range allTargets {
				if backendWeights[t.host] != nil {
					hasExplicitWeights = true
					break
				}
			}

			selector, err := NewSelector(algorithm, hasExplicitWeights)
			if err != nil {
				slog.Error("Error creating selector", "arg", i, "algorithm", algorithm, "err", err)
				os.Exit(1)
			}
			statsServer.RegisterSelector(porti, selector)

			host := allTargets[0].host
			port := allTargets[0].port

			if httpMode {
				transportMgr := listenerAndForwardHttp(porti, host, port, proxyConfig, false, tls.Certificate{}, pool, selector, affinity)
				statsServer.RegisterTransportManager(porti, transportMgr)
			} else if httpsMode {
				if cert == "" || key == "" {
					cert, key = generateSelfSignedCert()
					slog.Info("Self signed certificate generated", "cert", cert, "key", key)
				}
				cer, err := tls.LoadX509KeyPair(cert, key)
				if err != nil {
					slog.Error("Failed to load TLS certificate", "cert", cert, "key", key, "err", err)
					os.Exit(1)
				}
				transportMgr := listenerAndForwardHttp(porti, host, port, proxyConfig, true, cer, pool, selector, affinity)
				statsServer.RegisterTransportManager(porti, transportMgr)
			} else {
				listenAndForward(porti, pool, selector, affinity, proxyConfig)
			}
		} else {
			// Single-target mode: original behavior with port range expansion
			for j := range len(listenPorts) {
				porti := listenPorts[j]
				port := allTargets[j].port
				host := allTargets[j].host

				poolKey := host + ":" + port
				if pools[poolKey] == nil {
					pools[poolKey] = NewBackendPool(host, port)
					resolvers[host].Subscribe(pools[poolKey])
					slog.Info("Backend pool subscribed to DNS resolver", "host", host, "port", port)

					if weights, ok := backendWeights[host]; ok {
						pools[poolKey].SetWeights(weights)
					}
					statsServer.RegisterBackendPool(poolKey, pools[poolKey])
				}
				pool := pools[poolKey]

				var affinity *AffinityMap
				if affinityEnabled {
					if affinityMaps[host] == nil {
						ttl := *affinityTTL
						if ttl == 0 {
							ttl = 30 * time.Second
						}
						affinityMaps[host] = NewAffinityMap(host, ttl)
						slog.Info("IP affinity enabled", "host", host, "ttl", ttl)
						statsServer.RegisterAffinityMap(host, affinityMaps[host])
					}
					affinity = affinityMaps[host]
				}

				hasExplicitWeights := backendWeights[host] != nil
				selector, err := NewSelector(algorithm, hasExplicitWeights)
				if err != nil {
					slog.Error("Error creating selector", "arg", i, "algorithm", algorithm, "err", err)
					os.Exit(1)
				}
				statsServer.RegisterSelector(porti, selector)

				if httpMode {
					transportMgr := listenerAndForwardHttp(porti, host, port, proxyConfig, false, tls.Certificate{}, pool, selector, affinity)
					statsServer.RegisterTransportManager(porti, transportMgr)
				} else if httpsMode {
					if cert == "" || key == "" {
						cert, key = generateSelfSignedCert()
						slog.Info("Self signed certificate generated", "cert", cert, "key", key)
					}
					cer, err := tls.LoadX509KeyPair(cert, key)
					if err != nil {
						slog.Error("Failed to load TLS certificate", "cert", cert, "key", key, "err", err)
						os.Exit(1)
					}
					transportMgr := listenerAndForwardHttp(porti, host, port, proxyConfig, true, cer, pool, selector, affinity)
					statsServer.RegisterTransportManager(porti, transportMgr)
				} else {
					listenAndForward(porti, pool, selector, affinity, proxyConfig)
				}
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
	showVersion := flag.Bool("version", false, "Print version and exit")

	flag.Usage = func() {
		flagSet := flag.CommandLine
		fmt.Printf("Usage of %s: %s\n", os.Args[0], "<(port[-port2]:)?host:port[-port2](,option,...)? ...>")
		flagSet.PrintDefaults()
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("docker-lb version %s (build %s, %s)\n", Version, Build, Date)
		os.Exit(0)
	}

	slog.Info(os.Args[0], "build", Build, "version", Version, "date", Date)

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	smain(flag.Args(), *clientProxyProtocol, *serverProxyProtocol, *cert, *key)

	c := make(chan int)
	<-c
}
