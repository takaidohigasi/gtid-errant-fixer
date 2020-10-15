BINNAME=gtid-errant-fixer
VERSION:=$$(git describe --tags)
LDFLAGS=-ldflags "-X $$(head -n1 go.mod | awk '{print $$2}')/cmd.Version=$(VERSION) -w -s"

.PHONY: all
all: dep build

.PHONY: dep
dep:
	go mod download
	go mod tidy

.PHONY: linux
linux:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINNAME) main.go

.PHONY: build
build:
	CGO_ENABLED=1 GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINNAME) main.go

.PHONY: clean
clean:
	go clean
	go clean -testcache
	rm -f bin/$(BINNAME)
