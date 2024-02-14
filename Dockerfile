FROM golang:alpine
WORKDIR /src
COPY go.mod .
#COPY go.sum .
RUN go mod download
COPY . .
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build"  go build -ldflags="-s -w" -o /bin/lb ./src/main.go

FROM alpine
COPY --from=0 /bin/lb /bin/lb
CMD ["/bin/lb"]
