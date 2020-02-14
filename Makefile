# Required for globs to work correctly
SHELL=/bin/bash

# go option
GO         ?= go
PKG        := go mod download
LDFLAGS    := -w -s
GOFLAGS    :=
TAGS       := 
BINDIR     := $(CURDIR)/bin
PKGDIR     := $(shell grep module go.mod| awk '{ print $$2 }')
CGO_ENABLED := 1



.PHONY: all
all: build

.PHONY: dep
dep:
	@echo " ===> Installing dependencies via '$$(awk '{ print $$1 }' <<< "$(PKG)" )' <=== "
	@$(PKG)

.PHONY: build
build:
	@echo " ===> building releases in ./bin/... <=== "
	@mkdir -p '$(BINDIR)'
	GO11MODULE=enabled CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o '$(BINDIR)/' -v $(GOFLAGS) -tags '$(TAGS)' -ldflags '$(LDFLAGS)' '$(PKGDIR)/cmd/...'

.PHONY: docker-build
docker-build:
	DOCKER_BUILDKIT=1 docker build -t tritonmedia/downloader-go .

.PHONY: gofmt
gofmt:
	@echo " ===> Running goimports <==="
	goimports -w ./

.PHONY: test
test:
	go test $(PKGDIR)/...

.PHONY: render-circle
render-circle:
	@if [[ ! -e /tmp/jsonnet-libs ]]; then git clone git@github.com:tritonmedia/jsonnet-libs /tmp/jsonnet-libs; else cd /tmp/jsonnet-libs; git pull; fi
	JSONNET_PATH=/tmp/jsonnet-libs jsonnet .circleci/circle.jsonnet | yq . -y > .circleci/config.yml