.PHONY: build build-cli build-lib clean all test

BIN_DIR=bin
TOOLS_DIR=tools
LIBS_DIR=libs

all: build

build: build-cli build-lib

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/ioimg ./$(TOOLS_DIR)/ioimg
	go build -o $(BIN_DIR)/iovid ./$(TOOLS_DIR)/iovid

build-lib:
	@mkdir -p $(BIN_DIR)
	go build -buildmode=c-shared -o $(BIN_DIR)/libiocore.so ./$(LIBS_DIR)/iocore/bridge/main.go

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
