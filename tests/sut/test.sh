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