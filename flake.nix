{
  description = "Tyde — Lightweight Wayland desktop environment written in Go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        # The compositor targets the wlroots 0.20 API (pkg-config: wlroots-0.20).
        wlroots = pkgs.wlroots_0_20;

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
          wlroots

          # X11 (for X11 desktop mode and Fyne)
          xorg.libX11
          xorg.libXcursor
          xorg.libXrandr
          xorg.libXinerama
          xorg.libXi
          xorg.libXxf86vm
        ];

        nativeBuildInputs = with pkgs; [
          go
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
            echo "Tyde dev shell — Go $(go version | awk '{print $3}'), wlroots ${wlroots.version}"
            echo ""
            echo "  make compositor   Build Wayland compositor"
            echo "  make panel        Build panel"
            echo "  make ctl          Build tyde_wmctl"
            echo "  make test         Run tests"
            echo ""
          '';

          # CGO needs these
          CGO_ENABLED = "1";
          LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath buildInputs;
        };

        packages.default = pkgs.buildGoModule {
          pname = "tyde";
          version = "1.1.0";
          src = ./.;

          vendorHash = null; # uses vendor directory

          inherit buildInputs nativeBuildInputs;

          CGO_ENABLED = "1";

          buildPhase = ''
            make wayland
          '';

          installPhase = ''
            mkdir -p $out/bin
            install -m755 tyde_compositor $out/bin/tyde_compositor
            install -m755 tyde_panel $out/bin/tyde_panel
            install -m755 tyde_wmctl $out/bin/tyde_wmctl
            install -m755 tyde_emoji $out/bin/tyde_emoji
            install -m755 tyde_chooser $out/bin/tyde_chooser

            mkdir -p $out/share/wayland-sessions
            install -m644 tyde-wayland.desktop $out/share/wayland-sessions/

            mkdir -p $out/share/xdg-desktop-portal/portals
            install -m644 portals/tyde.portal $out/share/xdg-desktop-portal/portals/

            mkdir -p $out/share/man/man1 $out/share/man/man5
            install -m644 docs/man/tyde_compositor.1 $out/share/man/man1/
            install -m644 docs/man/tyde_panel.1 $out/share/man/man1/
            install -m644 docs/man/tyde_wmctl.1 $out/share/man/man1/
            install -m644 docs/man/tyde.5 $out/share/man/man5/
          '';

          meta = with pkgs.lib; {
            description = "Lightweight Wayland desktop environment written in Go";
            homepage = "https://github.com/FyshOS/tyde";
            license = licenses.bsd3;
            platforms = platforms.linux;
          };
        };
      }
    );
}
