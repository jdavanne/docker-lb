FROM --platform=$BUILDPLATFORM golang:alpine
ARG TARGETOS
ARG TARGETARCH
RUN apk add --no-cache git make
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build" GOOS=${TARGETOS} GOARCH=${TARGETARCH} make build

FROM alpine
COPY --from=0 /src/bin/lb /bin/lb
CMD ["/bin/lb"]
