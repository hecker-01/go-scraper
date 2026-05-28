.PHONY: build run test lint clean install

ifeq ($(OS),Windows_NT)
EXT=.exe
else
EXT=
endif

BUILD_NAME=go-scraper

build:
	go build -o $(BUILD_NAME)$(EXT) .

run:
	go run .

test:
	go test ./...

lint:
	go vet ./...

ifeq ($(OS),Windows_NT)
INSTALL_DIR=C:\Windows\System32
install: build
	move /Y $(BUILD_NAME)$(EXT) $(INSTALL_DIR)\$(BUILD_NAME)$(EXT)
else
INSTALL_DIR=/usr/local/bin
install: build
	@if [ -w $(INSTALL_DIR) ]; then \
		cp $(BUILD_NAME) $(INSTALL_DIR)/$(BUILD_NAME); \
	else \
		sudo cp $(BUILD_NAME) $(INSTALL_DIR)/$(BUILD_NAME); \
	fi
	@echo "Installed to $(INSTALL_DIR)/$(BUILD_NAME)"
endif

clean:
	go clean ./...
	rm -f $(BUILD_NAME) $(BUILD_NAME)$(EXT)
