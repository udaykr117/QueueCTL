.PHONY: build run clean

build:
	go build -o queuectl .

run:
	go run .

clean:
	rm -f queuectl
