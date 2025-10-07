#!/bin/bash
#

set -euo pipefail

echo "=== Checking stats API health ==="
curl -f -s http://lb:8080/health
echo ""

echo ""
echo "=== Backend pools discovered ==="
curl -f -s http://lb:8080/backends | jq '.'
echo ""

echo "=== Testing direct service1 ==="
for i in {1..10}; do
    curl -f -s http://service1:8081 | jq -c '{service, hostname, port, request_count}'
done

echo ""
echo "=== Testing lb service1 (TCP with random) ==="
for i in {1..10}; do
    curl -f -s http://lb:10123 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Stats for port 10123 after random load balancing ==="
curl -f -s http://lb:8080/backends | jq '.[] | select(.host == "service1" and .port == "8081")'
echo ""

echo ""
echo "=== Testing direct service2 ==="
for i in {1..10}; do
    curl -f -s http://service2:8081 | jq -c '{service, hostname, port, request_count}'
done

echo ""
echo "=== Testing lb service2 (HTTP with round-robin) ==="
for i in {1..10}; do
    curl -f -s http://lb:10124 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing lb service2 (HTTPS with least-connection) ==="
for i in {1..10}; do
    curl -f -s -k https://lb:10125 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing lb service2 with cookie affinity (session 1) ==="
curl -s --cookie-jar /tmp/cookies http://lb:10124 | jq -c '{service, hostname, port}'
for i in {1..10}; do
    curl -s --cookie /tmp/cookies http://lb:10124 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing lb service2 with cookie affinity (session 2) ==="
curl -s --cookie-jar /tmp/cookies2 http://lb:10124 | jq -c '{service, hostname, port}'
for i in {1..10}; do
    curl -s --cookie /tmp/cookies2 http://lb:10124 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing port range mapping on service3 ==="
echo "Testing port 10126 (maps to service3:9000)"
for i in {1..5}; do
    curl -f -s http://lb:10126 | jq -c '{service, hostname, port}'
done

echo ""
echo "Testing port 10127 (maps to service3:9001)"
for i in {1..5}; do
    curl -f -s http://lb:10127 | jq -c '{service, hostname, port}'
done

echo ""
echo "Testing port 10128 (maps to service3:9002)"
for i in {1..5}; do
    curl -f -s http://lb:10128 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing cookie affinity on port range - port 10126 ==="
curl -s --cookie-jar /tmp/cookies_10126 http://lb:10126 | jq -c '{service, hostname, port}'
for i in {1..10}; do
    curl -s --cookie /tmp/cookies_10126 http://lb:10126 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing cookie affinity on port range - port 10127 ==="
curl -s --cookie-jar /tmp/cookies_10127 http://lb:10127 | jq -c '{service, hostname, port}'
for i in {1..10}; do
    curl -s --cookie /tmp/cookies_10127 http://lb:10127 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing different load balancing algorithms ==="

echo ""
echo "Testing round-robin with affinity (port 10129)"
curl -s --cookie-jar /tmp/cookies_rr http://lb:10129 | jq -c '{service, hostname, port}'
for i in {1..5}; do
    curl -s --cookie /tmp/cookies_rr http://lb:10129 | jq -c '{service, hostname, port}'
done

echo ""
echo "Testing least-connection with affinity (port 10130)"
curl -s --cookie-jar /tmp/cookies_lc http://lb:10130 | jq -c '{service, hostname, port}'
for i in {1..5}; do
    curl -s --cookie /tmp/cookies_lc http://lb:10130 | jq -c '{service, hostname, port}'
done

echo ""
echo "Testing weighted-random with affinity (port 10131)"
curl -s --cookie-jar /tmp/cookies_wr http://lb:10131 | jq -c '{service, hostname, port}'
for i in {1..5}; do
    curl -s --cookie /tmp/cookies_wr http://lb:10131 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Testing load balancing algorithms without affinity (TCP) ==="

echo ""
echo "Testing round-robin without affinity (port 10132)"
for i in {1..10}; do
    curl -f -s http://lb:10132 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Stats for port 10132 after round-robin ==="
curl -f -s http://lb:8080/backends | jq '.[] | select(.host == "service1" and .port == "8081") | {host, port, backends: [.backends[] | {ip, active_conns, total_conns}]}'
echo ""

echo ""
echo "Testing least-connection without affinity (port 10133)"
for i in {1..10}; do
    curl -f -s http://lb:10133 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Stats for port 10133 after least-connection ==="
curl -f -s http://lb:8080/backends | jq '.[] | select(.host == "service2" and .port == "8081") | {host, port, backends: [.backends[] | {ip, active_conns, total_conns}]}'
echo ""

echo ""
echo "Testing weighted-random without affinity (port 10134)"
for i in {1..10}; do
    curl -f -s http://lb:10134 | jq -c '{service, hostname, port}'
done

echo ""
echo "=== Stats for port 10134 after weighted-random ==="
curl -f -s http://lb:8080/backends | jq '.[] | select(.host == "service3" and .port == "9000") | {host, port, backends: [.backends[] | {ip, active_conns, total_conns}]}'
echo ""

echo ""
echo "=== Final backend stats summary ==="
curl -f -s http://lb:8080/backends | jq '.[] | {host, port, backend_count: .count, total_requests: ([.backends[].total_conns] | add)}'
echo ""

echo ""
echo "All tests passed!"