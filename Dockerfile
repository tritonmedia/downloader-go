FROM golang:1.14-alpine3.11 AS builder
ENTRYPOINT ["/usr/bin/downloader"]
WORKDIR /src/app

# Build deps
RUN apk add --no-cache git make bash

# Install our dependencies
COPY go.mod go.sum ./
RUN go mod download

# build the application
COPY . /src/app
RUN make CGO_ENABLED=0

FROM alpine:3.13
RUN apk add --no-cache ca-certificates
COPY --from=builder /src/app/bin/downloader /usr/bin/downloader