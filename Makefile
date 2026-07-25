VERSION ?= dev

.PHONY: build test lint vuln docker-build docker-up docker-down clean

build:
	CGO_ENABLED=0 go build -ldflags="-w -s -X main.version=$(VERSION)" -o searxng-mcp ./cmd/searxng-mcp

test:
	go test -v -cover ./...

lint:
	go vet ./... && gofmt -l .

vuln:
	govulncheck ./...

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t searxng-mcp:latest .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

clean:
	rm -f searxng-mcp
