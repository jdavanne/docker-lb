FROM golang:alpine AS builder
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN go build -o /backend .

FROM alpine
COPY --from=builder /backend /backend
CMD ["/backend"]
