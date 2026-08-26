.PHONY: run build test test-race fmt vet lint tidy compose-up compose-down clean

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

fmt:
	gofmt -l -w .

vet:
	go vet ./...

lint: fmt vet

tidy:
	go mod tidy

compose-up:
	docker compose up --build

compose-down:
	docker compose down

clean:
	go clean
	rm -rf bin
