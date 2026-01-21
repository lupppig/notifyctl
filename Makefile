.PHONY: proto build run clean build-cli build-server

proto:
	protoc --go_out=. --go_opt=module=github.com/lupppig/notifyctl \
		--go-grpc_out=. --go-grpc_opt=module=github.com/lupppig/notifyctl \
		api/health/v1/health.proto \
		api/notify/v1/notify.proto

build: build-server build-cli

build-server:
	go build -o bin/server ./cmd/server

build-cli:
	go build -o bin/notifyctl ./cmd/notifyctl

run:
	go run ./cmd/server

clean:
	rm -rf bin/
