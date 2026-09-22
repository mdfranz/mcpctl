BINARY   := mcpctl
CMD      := ./cmd/mcpctl
PREFIX   := $(HOME)/.local/bin

.PHONY: all build test vet fmt fmt-check tidy run clean check install uninstall

all: check build

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "gofmt needed on:"; echo "$$fmt_out"; exit 1; \
	fi

tidy:
	go mod tidy

run:
	go run $(CMD) $(ARGS)

clean:
	rm -rf bin

check: fmt-check vet test

install: build
	mkdir -p $(PREFIX)
	install -m 0755 bin/$(BINARY) $(PREFIX)/$(BINARY)
	@echo "installed to $(PREFIX)/$(BINARY)"

uninstall:
	rm -f $(PREFIX)/$(BINARY)
