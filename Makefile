.PHONY: build run test lint clean

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

clean:
	go clean ./...
	rm -f $(BUILD_NAME) $(BUILD_NAME)$(EXT)
