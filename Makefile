.PHONY: build run clean test help

.DEFAULT_GOAL := help

BINARY_NAME=queuectl

build:
	go build -o $(BINARY_NAME) .

run:
	go run .

test:
	chmod +x test.sh
	./test.sh

clean:
	rm -f $(BINARY_NAME)
	rm -rf test_data/


