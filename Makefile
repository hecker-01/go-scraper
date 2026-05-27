.PHONY: build run test lint clean

build:
	go build -o go-scraper .

run:
	go run .

test:
	go test ./...

lint:
	go vet ./...

clean:
	go clean ./...
	rm -f go-scraper
