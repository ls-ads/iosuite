.PHONY: build build-cli build-lib clean all test build-all

BIN_DIR=bin
TOOLS_DIR=tools
LIBS_DIR=libs

platforms := linux windows darwin
architectures := amd64 arm64

all: build

build: build-cli build-lib

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/ioimg ./$(TOOLS_DIR)/ioimg
	go build -o $(BIN_DIR)/iovid ./$(TOOLS_DIR)/iovid

build-lib:
	@mkdir -p $(BIN_DIR)
	go build -buildmode=c-shared -o $(BIN_DIR)/libiocore.so ./$(LIBS_DIR)/iocore/bridge/main.go

build-all: $(foreach p,$(platforms),$(foreach a,$(architectures),build-ioimg-$(p)-$(a) build-iovid-$(p)-$(a)))

# Template for dynamic binary target generation
define BUILD_BINARY_TARGET
build-$(1)-$(2)-$(3):
	@mkdir -p $(BIN_DIR)
	GOOS=$(2) GOARCH=$(3) go build -o $(BIN_DIR)/$(1)-$(2)-$(3)$(if $(filter windows,$(2)),.exe,) ./$(TOOLS_DIR)/$(1)
endef

# Generate targets for ioimg and iovid across all platform/architecture combinations
$(foreach p,$(platforms),$(foreach a,$(architectures),$(eval $(call BUILD_BINARY_TARGET,ioimg,$(p),$(a)))))
$(foreach p,$(platforms),$(foreach a,$(architectures),$(eval $(call BUILD_BINARY_TARGET,iovid,$(p),$(a)))))

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
