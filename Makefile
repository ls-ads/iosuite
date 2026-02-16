.PHONY: build-cli build-lib clean all

BIN_DIR=bin
TOOLS_DIR=tools
LIBS_DIR=libs

all: build-cli build-lib

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/ioimg ./$(TOOLS_DIR)/ioimg

build-lib:
	@mkdir -p $(BIN_DIR)
	go build -buildmode=c-shared -o $(BIN_DIR)/libiocore.so ./$(LIBS_DIR)/iocore/bridge/main.go

clean:
	rm -rf $(BIN_DIR)
