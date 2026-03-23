{
  description = "FyneDesk — Lightweight Wayland desktop environment written in Go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        # wlroots 0.17.4 — the Go bindings require exactly 0.17.x
        wlroots_0_17 = pkgs.wlroots_0_17.overrideAttrs (old: rec {
          version = "0.17.4";
          src = pkgs.fetchFromGitLab {
            domain = "gitlab.freedesktop.org";
            owner = "wlroots";
            repo = "wlroots";
            rev = version;
            hash = "sha256-AntfO0s2/4PZbM7hBZXQkzQGKBJC/fFOPuuFbBVy8Yk=";
          };
        });

        buildInputs = with pkgs; [
          # Wayland core
          wayland
          wayland-protocols
          libxkbcommon

          # Rendering
          mesa
          libdrm
          libGL

          # Input
          libinput

          # Session
          seatd

          # Image processing
          pixman

          # XWayland
          xwayland
          xorg.libxcb
          xorg.xcbutilwm
          xorg.xcbutilrenderutil
          xorg.xcbutilerrors

          # wlroots
          wlroots_0_17

          # X11 (for X11 desktop mode and Fyne)
          xorg.libX11
          xorg.libXcursor
          xorg.libXrandr
          xorg.libXinerama
          xorg.libXi
          xorg.libXxf86vm
        ];

        nativeBuildInputs = with pkgs; [
          go_1_23
          gcc
          pkg-config
          meson
          ninja
        ];
      in
      {
        devShells.default = pkgs.mkShell {
          inherit buildInputs nativeBuildInputs;

          shellHook = ''
            echo "FyneDesk dev shell — Go $(go version | awk '{print $3}'), wlroots ${wlroots_0_17.version}"
            echo ""
            echo "  make compositor   Build Wayland compositor"
            echo "  make panel        Build panel"
            echo "  make ctl          Build fynedesk-ctl"
            echo "  make test         Run tests"
            echo ""
          '';

          # CGO needs these
          CGO_ENABLED = "1";
          LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath buildInputs;
        };

        packages.default = pkgs.buildGoModule {
          pname = "fynedesk";
          version = "1.1.0";
          src = ./.;

          vendorHash = null; # uses vendor directory

          inherit buildInputs nativeBuildInputs;

          CGO_ENABLED = "1";

          buildPhase = ''
            make compositor panel ctl
          '';

          installPhase = ''
            mkdir -p $out/bin
            install -m755 compositor $out/bin/fynedesk-compositor
            install -m755 fynedesk-panel $out/bin/fynedesk-panel
            install -m755 fynedesk-ctl $out/bin/fynedesk-ctl

            mkdir -p $out/share/wayland-sessions
            install -m644 fynedesk-wayland.desktop $out/share/wayland-sessions/

            mkdir -p $out/share/xdg-desktop-portal/portals
            install -m644 portals/fynedesk.portal $out/share/xdg-desktop-portal/portals/

            mkdir -p $out/share/man/man1 $out/share/man/man5
            install -m644 docs/man/fynedesk-compositor.1 $out/share/man/man1/
            install -m644 docs/man/fynedesk-panel.1 $out/share/man/man1/
            install -m644 docs/man/fynedesk-ctl.1 $out/share/man/man1/
            install -m644 docs/man/fynedesk.5 $out/share/man/man5/
          '';

          meta = with pkgs.lib; {
            description = "Lightweight Wayland desktop environment written in Go";
            homepage = "https://github.com/nicholasgasior/fynedesk";
            license = licenses.bsd3;
            platforms = platforms.linux;
          };
        };
      }
    );
}
