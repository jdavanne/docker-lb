FROM golang:alpine AS builder
WORKDIR /src
COPY go.mod .
COPY main.go .
RUN go build -o /backend main.go

FROM alpine
COPY --from=builder /backend /backend
CMD ["/backend"]
