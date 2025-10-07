#!/bin/bash
#

set -euo pipefail

echo "direct service1" 
for i in {1..10}; do
    curl -f http://service1:8081
done

echo "lb service1"
for i in {1..10}; do
    curl -f http://lb:10123
done

curl -f http://lb:10123
curl -f http://lb:10123
curl -f http://lb:10123

echo "direct service2"
for i in {1..10}; do
    curl -f http://service2:8081
done

echo "lb service2 : should be only one..."
curl --cookie-jar /tmp/cookies http://lb:10124
for i in {1..10}; do
    curl --cookie /tmp/cookies http://lb:10124
done

echo "lb service2 : should be only one..."
curl --cookie-jar /tmp/cookies2 http://lb:10124
for i in {1..10}; do
    curl --cookie /tmp/cookies2 http://lb:10124
done

echo "Testing port range mapping on service3"
echo "Testing port 10126 (maps to service3:9000)"
for i in {1..5}; do
    curl -f http://lb:10126
done

echo "Testing port 10127 (maps to service3:9001)"
for i in {1..5}; do
    curl -f http://lb:10127
done

echo "Testing port 10128 (maps to service3:9002)"
for i in {1..5}; do
    curl -f http://lb:10128
done

echo "Testing cookie affinity on port range - port 10126"
curl --cookie-jar /tmp/cookies_10126 http://lb:10126
for i in {1..10}; do
    curl --cookie /tmp/cookies_10126 http://lb:10126
done

echo "Testing cookie affinity on port range - port 10127"
curl --cookie-jar /tmp/cookies_10127 http://lb:10127
for i in {1..10}; do
    curl --cookie /tmp/cookies_10127 http://lb:10127
done

echo "All tests passed!"