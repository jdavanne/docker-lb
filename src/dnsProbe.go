package main

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"sync"
	"time"
)

type DnsProbe struct {
	host   string
	ips    map[string]int
	lck    sync.RWMutex
	ipList []string
}

func NewDnsProbe(host string) *DnsProbe {
	return &DnsProbe{host: host, ips: map[string]int{}}
}

func (p *DnsProbe) resolve() (string, error) {
	p.lck.RLock()
	defer p.lck.RUnlock()
	N := len(p.ipList)
	if N == 0 {
		return "", fmt.Errorf("not found")
	}
	n := rand.IntN(N)
	return p.ipList[n], nil
}

func (p *DnsProbe) checkIp(ip string) bool {
	p.lck.RLock()
	defer p.lck.RUnlock()
	return p.ips[ip] != 0
}

func (p *DnsProbe) dnsProbe() {
	slog.Info("Resolving", "host", p.host)
	round := 0
	for {
		if round != 0 {
			time.Sleep(*probePeriod)
		}
		round++
		changed := 0
		if *verbose {
			PrintMemUsage()
			slog.Info("Probing...", "host", p.host)
		}

		ips, err := net.LookupIP(p.host)
		if err != nil {
			slog.Error("Lookup failed", "host", p.host, "err", err)
			continue
		}
		p.lck.Lock()
		for _, ip := range ips {
			if p.ips[ip.String()] == 0 {
				changed++
				slog.Info("New", "host", p.host, "ip", ip.String())
			}
			p.ips[ip.String()] = round
		}

		for k, v := range p.ips {
			if v != round {
				changed++
				slog.Info("Lost", "host", p.host, "ip", k)
				delete(p.ips, k)
			}
		}
		if changed != 0 {
			l := make([]string, 0, len(p.ips))
			for ip := range p.ips {
				l = append(l, ip)
			}
			p.ipList = l
		}
		p.lck.Unlock()
	}
}
