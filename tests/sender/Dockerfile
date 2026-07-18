FROM golang:alpine AS builder
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN go build -o /sender .

FROM alpine
COPY --from=builder /sender /sender
CMD ["/sender"]
