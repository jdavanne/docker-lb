# docker-lb

This is a very simple TCP Load Balancer, that resolve dns on the fly.

The typical use case is for `docker compose scale`

## Usage

`docker-compose.yml`:
```yml
version: "3"
services:
  lb:
    build: ./
    image: docker-lb
    ports:
      - "8080:8080"
    command: ["/bin/lb", "--verbose",  "8080:service1:8081"]

  service1:
    image: alpine
    command: ["/bin/sh", "-c", "while true; do echo 'HTTP/1.1 200 OK\n\nHello, world from '$$HOSTNAME'!' | nc -l -p 8081; done"]
```

```sh
    docker-compose scale service1=4
    curl http://localhost:8080 # Will be load balanced across nodes
```

## Possible extension
- add a true load balanced algorithm : 
  - random,
  - round-robin,
  - least-connection,
  - weighted-least connection
  - partitioned
- multiple service target (blue/green)
- proxy
- bandwidth limits
- latency increase
- network failures : random failure, packet split, conecction hanging
- http/https : cookie based affinity



