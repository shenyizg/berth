VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build install test e2e vet fmt check clean

all: check build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/berth ./cmd/berth

install:
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/berth

test:
	go test ./...

e2e:
	BERTH_E2E=1 go test -count=1 -v ./e2e

vet:
	go vet ./...

fmt:
	gofmt -w .

check: vet
	@unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt (run make fmt):"; echo "$$unformatted"; exit 1; fi
	go test ./...

clean:
	rm -rf bin dist
