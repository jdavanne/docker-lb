
include .env

VERSION := ${VERSION}
NAME := lb
DATE := $(shell date +'%Y-%m-%d_%H:%M:%S')
BUILD := $(shell git rev-parse HEAD | cut -c1-8)
LDFLAGS :=-ldflags '-s -w  -X=main.Version=$(VERSION) -X=main.Build=$(BUILD) -X=main.Date=$(DATE)'

build: 
	CGO_ENABLED=0 go build $(LDFLAGS) -o ./bin/$(NAME) ./src

docker:
	docker buildx build --platform ${PLATFORM} -t davinci1976/docker-lb:latest .

docker-push:
	docker image tag davinci1976/docker-lb:latest davinci1976/docker-lb:${VERSION}
	docker push --all-tags davinci1976/docker-lb

docker-push-dev:
	docker push --platform ${PLATFORM} davinci1976/docker-lb:latest
 
docker-test:
	docker compose -f docker-compose.test.yml run --rm --remove-orphans --build sut 
