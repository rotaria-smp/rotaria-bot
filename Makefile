BIN_DIR := bin
TARGET := $(BIN_DIR)/rotaria-bot
SRC := ./cmd/bot

.PHONY: all build test clean

all: build

build:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(TARGET) $(SRC)

clean:
	rm -rf $(BIN_DIR)

test:
	go test ./...