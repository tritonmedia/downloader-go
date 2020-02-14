FROM golang:1.13-alpine3.11 AS builder
ENTRYPOINT ["/usr/bin/downloader"]
WORKDIR /src/app

# Build deps
RUN apk add --no-cache git make bash

# Install our dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . /src/app
RUN make CGO_ENABLED=0

FROM alpine:3.11
RUN apk add --no-cache ca-certificates
COPY --from=builder /src/app/bin/downloader /usr/bin/downloader