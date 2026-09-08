.PHONY: run build test test-race bench fmt vet lint tidy compose-up compose-down clean

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test -v ./... -count=1

test-race:
	go test ./... -race -count=1

bench:
	go test -run=^$$ -bench=. -benchmem ./internal/game/...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

lint: fmt vet
	golangci-lint run ./...

tidy:
	go mod tidy

compose-up:
	docker compose up --build

compose-down:
	docker compose down

clean:
	go clean ./...
	-rm -rf bin
