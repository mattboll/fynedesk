# If PREFIX isn't provided, we check for /usr/local and use that if it exists.
# Otherwise we fall back to using /usr.

LOCAL ?= $(shell test -d $(DESTDIR)/usr/local && printf "/local" || printf "")
PREFIX ?= /usr$(LOCAL)

# --- X11 desktop (default, no wlroots dependency) ---

build:
	go build ./cmd/tyde_runner
	go build ./cmd/tyde_ctl
	go build ./cmd/tyde

clean:
	rm -f tyde tyde_ctl tyde_runner tyde_compositor tyde_panel tyde_wmctl tyde_emoji tyde_chooser
	rm -f coverage.out

install:
	install -Dm00755 tyde_runner $(DESTDIR)$(PREFIX)/bin/tyde_runner
	install -Dm00755 tyde_ctl $(DESTDIR)$(PREFIX)/bin/tyde_ctl
	install -Dm00755 tyde $(DESTDIR)$(PREFIX)/bin/tyde
	install -Dm00644 theme/assets/icon.png $(DESTDIR)$(PREFIX)/share/pixmaps/com.fyshos.tyde.png
	install -Dm00644 tyde.desktop $(DESTDIR)$(PREFIX)/share/xsessions/tyde.desktop
	install -Dm00644 tyde-welcome.desktop $(DESTDIR)$(PREFIX)/share/applications/tyde-welcome.desktop
	install -Dm00644 tyde-fathom.desktop $(DESTDIR)$(PREFIX)/share/applications/tyde-fathom.desktop

uninstall:
	-rm $(DESTDIR)$(PREFIX)/bin/tyde_runner
	-rm $(DESTDIR)$(PREFIX)/bin/tyde_ctl
	-rm $(DESTDIR)$(PREFIX)/bin/tyde
	-rm $(DESTDIR)$(PREFIX)/share/xsessions/tyde.desktop
	-rm $(DESTDIR)$(PREFIX)/share/applications/tyde-welcome.desktop
	-rm $(DESTDIR)$(PREFIX)/share/applications/tyde-fathom.desktop

embed:
	Xephyr :5 -screen 1280x720 &
	DISPLAY=:5 go run -tags migrated_fynedo ./cmd/tyde

# === FyshOS packaging =========================================================
DEB_VERSION     ?=
DEB_NAME        ?= tyde
DEB_SECTION     ?= x11
DEB_DESCRIPTION ?= FyshOS desktop environment
DEB_HOMEPAGE    ?= https://fyshos.com
DEB_SUDO        ?= -sudo
DEB_BUILD_DEPS  ?= libgl1-mesa-dev xorg-dev libpam0g-dev libwayland-dev \
                   libxkbcommon-dev libglib2.0-dev libgbm-dev

repo:
	fyshpkg make \
		-name "$(DEB_NAME)" \
		$(if $(DEB_VERSION),-version "$(DEB_VERSION)") \
		-section "$(DEB_SECTION)" \
		-description "$(DEB_DESCRIPTION)" \
		-homepage "$(DEB_HOMEPAGE)" \
		-build-deps "$(DEB_BUILD_DEPS)" \
		$(DEB_SUDO) $(FYSHPKG_FLAGS) \
		.

# --- Wayland compositor (requires wlroots 0.20, see: make setup-wayland) ---
#
# wlroots is found with pkg-config (module wlroots-0.20): the distribution
# package when installed, otherwise a copy built into ~/.local by
# scripts/setup-wayland.sh (searched first, and added to the rpath).

MULTIARCH ?= $(shell dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo $(shell uname -m)-linux-gnu)
WLROOTS_PC = wlroots-0.20
WLROOTS_PKG_CONFIG = $(HOME)/.local/lib/$(MULTIARCH)/pkgconfig
WLROOTS_LIB = $(HOME)/.local/lib/$(MULTIARCH)
WLROOTS_ENV = PKG_CONFIG_PATH="$(WLROOTS_PKG_CONFIG):$$PKG_CONFIG_PATH" \
              CGO_LDFLAGS="-Wl,-rpath,$(WLROOTS_LIB)" \
              LD_LIBRARY_PATH="$(WLROOTS_LIB):$$LD_LIBRARY_PATH"

setup-wayland:
	./scripts/setup-wayland.sh

check-wlroots:
	@$(WLROOTS_ENV) pkg-config --exists $(WLROOTS_PC) || { \
		echo "$(WLROOTS_PC) not found: install libwlroots-0.20-dev (Debian), wlroots0.20 (Arch)"; \
		echo "or wlroots-devel 0.20 (Fedora), or run: make setup-wayland"; exit 1; }

compositor: check-wlroots
	$(WLROOTS_ENV) go build -o tyde_compositor ./cmd/tyde_compositor

panel:
	$(WLROOTS_ENV) go build -o tyde_panel ./cmd/tyde_panel
	@test -f $(HOME)/.local/bin/tyde_panel && \
		mv -f $(HOME)/.local/bin/tyde_panel $(HOME)/.local/bin/tyde_panel.old && \
		cp tyde_panel $(HOME)/.local/bin/tyde_panel && \
		rm -f $(HOME)/.local/bin/tyde_panel.old \
		|| true

ctl:
	go build -o tyde_wmctl ./cmd/tyde_wmctl
	@mkdir -p $(HOME)/.local/bin && \
		cp tyde_wmctl $(HOME)/.local/bin/tyde_wmctl || true

emoji-picker:
	go build -o tyde_emoji ./cmd/tyde_emoji
	cp tyde_emoji cmd/tyde_emoji/tyde_emoji
	@mkdir -p $(HOME)/.local/bin && \
		cp tyde_emoji $(HOME)/.local/bin/tyde_emoji || true

# Build all Wayland components.
chooser:
	go build -o tyde_chooser ./cmd/tyde_chooser

wayland: compositor panel ctl emoji-picker chooser

# Usage: make wayland && sudo make wayland-install
# Build BEFORE sudo — wayland-install only copies pre-built binaries.
wayland-install:
	install -Dm00755 tyde_compositor $(DESTDIR)$(PREFIX)/bin/tyde_compositor
	install -Dm00755 tyde_panel $(DESTDIR)$(PREFIX)/bin/tyde_panel
	install -Dm00755 tyde_wmctl $(DESTDIR)$(PREFIX)/bin/tyde_wmctl
	install -Dm00755 tyde_emoji $(DESTDIR)$(PREFIX)/bin/tyde_emoji
	install -Dm00755 tyde_chooser $(DESTDIR)$(PREFIX)/bin/tyde_chooser
	install -Dm00644 tyde-wayland.desktop $(DESTDIR)$(PREFIX)/share/wayland-sessions/tyde-wayland.desktop
	install -Dm00644 portals/tyde.portal $(DESTDIR)$(PREFIX)/share/xdg-desktop-portal/portals/tyde.portal
	install -Dm00644 docs/man/tyde_compositor.1 $(DESTDIR)$(PREFIX)/share/man/man1/tyde_compositor.1
	install -Dm00644 docs/man/tyde_panel.1 $(DESTDIR)$(PREFIX)/share/man/man1/tyde_panel.1
	install -Dm00644 docs/man/tyde_wmctl.1 $(DESTDIR)$(PREFIX)/share/man/man1/tyde_wmctl.1
	install -Dm00644 docs/man/tyde.5 $(DESTDIR)$(PREFIX)/share/man/man5/tyde.5

wayland-run: compositor panel
	$(WLROOTS_ENV) WLR_BACKENDS=wayland ./tyde_compositor

wayland-clean:
	rm -f tyde_compositor tyde_panel tyde_wmctl tyde_emoji tyde_chooser

wayland-watch:
	@command -v entr >/dev/null 2>&1 || { echo "Install entr: apt install entr"; exit 1; }
	find cmd/tyde_compositor internal/wayland -name '*.go' | entr -r $(MAKE) wayland-run

# --- Testing (safe, avoids compositor CGO crash) ---

test:
	./scripts/test.sh

test-coverage:
	./scripts/test.sh -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1

# Integration tests: launches compositor headless and tests IPC via tyde_wmctl.
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

# Go sources and packages checked without wlroots (the compositor and its
# wlroots layer are checked by the CI compositor job).
GO_SOURCES = $(shell find . -name '*.go' -not -path './.local/*' -not -path './vendor/*')
LINT_PACKAGES = $(shell go list ./... | grep -v -e /cmd/tyde_compositor -e /internal/wayland/compositor -e /internal/wayland/wlr)

lint:
	@echo "--- goimports ---"
	@test -z "$$(goimports -e -l $(GO_SOURCES) | tee /dev/stderr)"
	@echo "--- go vet ---"
	go vet -tags ci $(LINT_PACKAGES)
	@echo "--- gocyclo (max 30) ---"
	gocyclo -over 30 -ignore "\.local/" .
	@echo "Lint complete."

fmt:
	goimports -w $(GO_SOURCES)

.PHONY: build clean install uninstall embed repo setup-wayland check-wlroots wayland compositor panel ctl emoji-picker chooser wayland-install wayland-run wayland-clean wayland-watch test test-coverage integration-test deb rpm lint fmt
