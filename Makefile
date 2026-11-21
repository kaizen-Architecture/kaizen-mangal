MAKEFLAGS += --silent

ldflags := -X 'github.com/kaizen-architecture/kaizen-mangal/constant.BuiltAt=$(shell date -u)'
ldflags += -X 'github.com/kaizen-architecture/kaizen-mangal/constant.BuiltBy=$(shell whoami)'
ldflags += -X 'github.com/kaizen-architecture/kaizen-mangal/constant.Revision=$(shell git rev-parse --short HEAD)'
ldflags += -s
ldflags += -w

build_flags := -ldflags=${ldflags}

all: help

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build        Build the kaizen-mangal binary"
	@echo "  install      Install the kaizen-mangal binary"
	@echo "  uninstall    Uninstall the kaizen-mangal binary"
	@echo "  test         Run the tests"
	@echo "  gif          Generate usage gifs"
	@echo "  help         Show this help message"
	@echo ""

install:
	@go install "$(build_flags)"

build:
	@go build -o kaizen-mangal "$(build_flags)"

test:
	@go test ./...

uninstall:
	@rm -f $(shell which kaizen-mangal)

gif:
	@vhs assets/tui.tape
	@vhs assets/inline.tape
