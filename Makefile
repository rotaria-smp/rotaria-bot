GOOS        ?= $(shell go env GOOS)
GOARCH      ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0
VERSION     ?= dev
BIN_DIR     := bin
DIST_DIR    := dist
TARGET      := $(BIN_DIR)/rotaria-bot
RELEASE_BIN := $(DIST_DIR)/rotaria-bot_$(GOOS)_$(GOARCH)
SRC         := ./cmd/bot
LDFLAGS     ?= -s -w
.RECIPEPREFIX := >

.PHONY: all build release build-release test clean build-all

all: build

build:
>mkdir -p $(BIN_DIR)
>CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="$(LDFLAGS)" -o $(TARGET) $(SRC)

build-release: clean
>mkdir -p $(DIST_DIR)
>CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="$(LDFLAGS)" -o $(RELEASE_BIN) $(SRC)
>chmod +x $(RELEASE_BIN)
>@echo "Built $(RELEASE_BIN) (VERSION=$(VERSION), GOOS=$(GOOS), GOARCH=$(GOARCH))"

release: build-release

build-all:
>@set -e; for arch in amd64 arm64; do for os in linux; do echo "==> $$os/$$arch"; GOOS=$$os GOARCH=$$arch $(MAKE) build-release; done; done

test:
>go test ./...

clean:
>rm -rf $(BIN_DIR) $(DIST_DIR)