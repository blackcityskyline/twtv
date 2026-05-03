BIN     := twtv
DESTDIR := $(HOME)/.local/bin
BUILD   := ./cmd/twtv

.PHONY: build install clean

build:
	go build -o $(BIN) $(BUILD)

install: build
	@mkdir -p $(DESTDIR)
	cp $(BIN) $(DESTDIR)/$(BIN)
	@echo "installed → $(DESTDIR)/$(BIN)"
	@# config skeleton is created on first run

clean:
	rm -f $(BIN)
