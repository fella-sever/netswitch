
.PHONY: build, local


all:

	make build
	make local

build:
	@echo "building..."
	env GOOS=linux GOARCH=arm go build -o netSwitcher -ldflags "-s -w" ./cmd/main.go
	@echo "done..."

local:

	@echo "transmitting to arm..."
	scp -P 2125 netSwitcher orange@192.168.0.5:/home/orange
	@echo "transmitted"
