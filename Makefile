.PHONY: proto build run clean

proto:
	protoc --go_out=. --go_opt=module=github.com/lupppig/notifyctl \
		--go-grpc_out=. --go-grpc_opt=module=github.com/lupppig/notifyctl \
		api/health/v1/health.proto \
		api/notify/v1/notify.proto

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

clean:
	rm -rf bin/
