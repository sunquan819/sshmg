.PHONY: build run dev clean test linux windows desktop

BINARY_NAME=deploy-manager

build:
	go build -o $(BINARY_NAME) ./cmd/server/main.go

run:
	go run ./cmd/server/main.go

dev:
	go run ./cmd/server/main.go --port 3001

desktop:
	cd cmd/desktop && wails build -clean

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux ./cmd/server/main.go

windows:
	GOOS=windows GOARCH=amd64 go build -o $(BINARY_NAME).exe ./cmd/server/main.go

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-linux $(BINARY_NAME).exe
	rm -rf ./data

test:
	go test ./...

deps:
	go mod tidy
	go mod download
