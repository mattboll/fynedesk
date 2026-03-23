# If PREFIX isn't provided, we check for /usr/local and use that if it exists.
# Otherwise we fall back to using /usr.

LOCAL ?= $(shell test -d $(DESTDIR)/usr/local && printf "/local" || printf "")
PREFIX ?= /usr$(LOCAL)

# --- X11 desktop (default, no wlroots dependency) ---

build:
	go build ./cmd/fynedesk_runner
	go build ./cmd/fynedesk

clean:
	rm -f fynedesk fynedesk_runner compositor fynedesk-panel fynedesk-ctl fynedesk-emoji
	rm -f coverage.out

install:
	install -Dm00755 fynedesk_runner $(DESTDIR)$(PREFIX)/bin/fynedesk_runner
	install -Dm00755 fynedesk $(DESTDIR)$(PREFIX)/bin/fynedesk
	install -Dm00644 fynedesk.desktop $(DESTDIR)$(PREFIX)/share/xsessions/fynedesk.desktop

uninstall:
	-rm $(DESTDIR)$(PREFIX)/bin/fynedesk_runner
	-rm $(DESTDIR)$(PREFIX)/bin/fynedesk
	-rm $(DESTDIR)$(PREFIX)/share/xsessions/fynedesk.desktop

embed:
	Xephyr :5 -screen 1280x720 &
	DISPLAY=:5 go run ./cmd/fynedesk

# --- Wayland compositor (requires wlroots 0.17, see: make setup-wayland) ---

MULTIARCH ?= $(shell dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo $(shell uname -m)-linux-gnu)
WLROOTS_PKG_CONFIG = $(HOME)/.local/lib/$(MULTIARCH)/pkgconfig
WLROOTS_LIB = $(HOME)/.local/lib/$(MULTIARCH)
WLROOTS_INC = $(HOME)/.local/include
WLROOTS_ENV = PKG_CONFIG_PATH="$(WLROOTS_PKG_CONFIG):$$PKG_CONFIG_PATH" \
              CGO_CFLAGS="-I$(WLROOTS_INC)" \
              CGO_LDFLAGS="-L$(WLROOTS_LIB) -Wl,-rpath,$(WLROOTS_LIB)" \
              LD_LIBRARY_PATH="$(WLROOTS_LIB):$$LD_LIBRARY_PATH"

setup-wayland:
	./scripts/setup-wayland.sh

compositor:
	$(WLROOTS_ENV) go build -o compositor ./cmd/compositor

panel:
	$(WLROOTS_ENV) go build -o fynedesk-panel ./cmd/fynedesk-panel
	rm -f cmd/fynedesk-panel/fynedesk-panel && cp fynedesk-panel cmd/fynedesk-panel/fynedesk-panel
	@test -f $(HOME)/.local/bin/fynedesk-panel && \
		mv -f $(HOME)/.local/bin/fynedesk-panel $(HOME)/.local/bin/fynedesk-panel.old && \
		cp fynedesk-panel $(HOME)/.local/bin/fynedesk-panel && \
		rm -f $(HOME)/.local/bin/fynedesk-panel.old \
		|| true

ctl:
	go build -o fynedesk-ctl ./cmd/fynedesk-ctl
	@mkdir -p $(HOME)/.local/bin && \
		cp fynedesk-ctl $(HOME)/.local/bin/fynedesk-ctl || true

emoji-picker:
	go build -o fynedesk-emoji ./cmd/fynedesk-emoji
	cp fynedesk-emoji cmd/fynedesk-emoji/fynedesk-emoji
	@mkdir -p $(HOME)/.local/bin && \
		cp fynedesk-emoji $(HOME)/.local/bin/fynedesk-emoji || true

# Build all Wayland components.
wayland: compositor panel ctl emoji-picker

# Usage: make wayland && sudo make wayland-install
# Build BEFORE sudo — wayland-install only copies pre-built binaries.
wayland-install:
	install -Dm00755 compositor $(DESTDIR)$(PREFIX)/bin/fynedesk-compositor
	install -Dm00755 fynedesk-panel $(DESTDIR)$(PREFIX)/bin/fynedesk-panel
	install -Dm00755 fynedesk-ctl $(DESTDIR)$(PREFIX)/bin/fynedesk-ctl
	install -Dm00755 fynedesk-emoji $(DESTDIR)$(PREFIX)/bin/fynedesk-emoji
	install -Dm00644 fynedesk-wayland.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/fynedesk-wayland.desktop
	install -Dm00644 portals/fynedesk.portal $(DESTDIR)$(PREFIX)/share/xdg-desktop-portal/portals/fynedesk.portal
	install -Dm00644 docs/man/fynedesk-compositor.1 $(DESTDIR)$(PREFIX)/share/man/man1/fynedesk-compositor.1
	install -Dm00644 docs/man/fynedesk-panel.1 $(DESTDIR)$(PREFIX)/share/man/man1/fynedesk-panel.1
	install -Dm00644 docs/man/fynedesk-ctl.1 $(DESTDIR)$(PREFIX)/share/man/man1/fynedesk-ctl.1
	install -Dm00644 docs/man/fynedesk.5 $(DESTDIR)$(PREFIX)/share/man/man5/fynedesk.5

wayland-run: compositor panel
	$(WLROOTS_ENV) WLR_BACKENDS=wayland ./compositor

wayland-clean:
	rm -f compositor fynedesk-panel fynedesk-ctl

wayland-watch:
	@command -v entr >/dev/null 2>&1 || { echo "Install entr: apt install entr"; exit 1; }
	find cmd/compositor internal/wayland -name '*.go' | entr -r $(MAKE) wayland-run

# --- Testing (safe, avoids compositor CGO crash) ---

test:
	./scripts/test.sh

test-coverage:
	./scripts/test.sh -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1

# Integration tests: launches compositor headless and tests IPC via fynedesk-ctl.
# Requires: make compositor ctl
integration-test: compositor ctl
	./scripts/integration-test.sh

# Build .deb package (requires: make compositor panel ctl first)
deb: compositor panel ctl
	./scripts/build-deb.sh

# Build .rpm package (requires: make compositor panel ctl first, rpmbuild installed)
rpm: compositor panel ctl
	./scripts/build-rpm.sh

# --- Linting (safe, avoids compositor CGO) ---

# Packages safe for static analysis (excludes cmd/compositor/ and internal/wayland/compositor/).
LINT_PACKAGES = ./internal/ui/ ./internal/emoji/ ./internal/icon/ ./internal/x11/... \
                ./modules/... ./wm/ ./wlipc/ ./theme/ \
                ./cmd/fynedesk/ ./cmd/fynedesk_runner/ ./cmd/fynedesk-panel/ \
                ./cmd/fynedesk-ctl/ ./cmd/fynedesk-emoji/

lint:
	@echo "--- goimports ---"
	@goimports -e -d $(shell find . -name '*.go' -not -path './cmd/compositor/*' -not -path './internal/wayland/compositor/*' -not -path './_fyne_patch/*' -not -path './vendor/*') || true
	@echo "--- go vet ---"
	go vet -tags ci $(LINT_PACKAGES)
	@echo "--- gocyclo (max 25) ---"
	@gocyclo -over 25 $(shell find . -name '*.go' -not -path './cmd/compositor/*' -not -path './internal/wayland/compositor/*' -not -path './_fyne_patch/*' -not -path './vendor/*') || true
	@echo "Lint complete."

fmt:
	goimports -w $(shell find . -name '*.go' -not -path './cmd/compositor/*' -not -path './internal/wayland/compositor/*' -not -path './_fyne_patch/*' -not -path './vendor/*')

.PHONY: build clean install uninstall embed setup-wayland wayland compositor panel ctl emoji-picker wayland-install wayland-run wayland-clean wayland-watch test test-coverage integration-test deb rpm lint fmt
