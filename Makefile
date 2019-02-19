.PHONY: clean checks test build

SRCS = $(shell git ls-files '*.go' | grep -v '^vendor/')

TAG_NAME := $(shell git tag -l --contains HEAD)
SHA := $(shell git rev-parse HEAD)
VERSION := $(if $(TAG_NAME),$(TAG_NAME),$(SHA))
BUILD_DATE := $(shell date -u '+%Y-%m-%d_%I:%M:%S%p')

default: clean checks test build

clean:
	rm -rf dist/ builds/ cover.out

build: clean
	@echo Version: $(VERSION)
	GO111MODULE=on go build -v -ldflags '-X "main.version=${VERSION}" -X "main.commit=${SHA}" -X "main.date=${BUILD_DATE}"'

test: clean
	GO111MODULE=on go test -v -cover ./...

checks:
	golangci-lint run

fmt:
	gofmt -s -l -w $(SRCS)
