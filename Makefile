BIN         := twtv
NOTIFY      := twtv-notify
PREFIX      := $(HOME)/.local/bin
SYSTEMD_DIR := $(HOME)/.config/systemd/user

GO          := go

.PHONY: all build build-notify install install-notify install-service \
        enable-service uninstall clean

all: build build-notify

build:
	$(GO) build -o $(BIN) ./cmd/twtv

build-notify:
	$(GO) build -ldflags="-s -w" -o $(NOTIFY) ./cmd/twtv-notify

install: build
	install -Dm755 $(BIN) $(PREFIX)/$(BIN)

install-notify: build-notify
	install -Dm755 $(NOTIFY) $(PREFIX)/$(NOTIFY)

install-service: install-notify
	install -Dm644 cmd/twtv-notify/twtv-notify.service $(SYSTEMD_DIR)/twtv-notify.service
	systemctl --user daemon-reload
	@echo "Run: systemctl --user enable --now twtv-notify"

enable-service:
	systemctl --user enable --now twtv-notify

uninstall:
	rm -f $(PREFIX)/$(BIN) $(PREFIX)/$(NOTIFY)
	systemctl --user disable --now twtv-notify 2>/dev/null || true
	rm -f $(SYSTEMD_DIR)/twtv-notify.service
	systemctl --user daemon-reload

clean:
	rm -f $(BIN) $(NOTIFY)
