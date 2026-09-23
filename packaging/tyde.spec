%global goipath     fyshos.com/tyde
%global forgeurl    https://github.com/FyshOS/tyde

Name:           tyde
Version:        1.1.0
Release:        1%{?dist}
Summary:        Lightweight Wayland desktop environment written in Go

License:        BSD-3-Clause
URL:            %{forgeurl}
Source0:        %{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.25
BuildRequires:  gcc
BuildRequires:  pkgconfig(wayland-server)
BuildRequires:  pkgconfig(wayland-client)
BuildRequires:  pkgconfig(wayland-protocols)
BuildRequires:  pkgconfig(xkbcommon)
BuildRequires:  pkgconfig(libinput)
BuildRequires:  pkgconfig(pixman-1)
BuildRequires:  pkgconfig(egl)
BuildRequires:  pkgconfig(glesv2)
BuildRequires:  pkgconfig(libseat)
BuildRequires:  pkgconfig(libdrm)
BuildRequires:  pkgconfig(gbm)
BuildRequires:  pkgconfig(gl)
BuildRequires:  pkgconfig(wlroots-0.20)

Requires:       wlroots%{?_isa} >= 0.20
Requires:       libwayland-server
Requires:       libxkbcommon
Requires:       libinput
Requires:       pixman
Requires:       mesa-libEGL
Requires:       mesa-libGLES
Requires:       xorg-x11-server-Xwayland
Requires:       libseat

Recommends:     foot
Recommends:     pipewire
Recommends:     wireplumber
Recommends:     grim
Recommends:     slurp

Suggests:       papirus-icon-theme
Suggests:       google-noto-sans-fonts

%description
Tyde is a full-featured Wayland compositor built with wlroots 0.20
and the Fyne GUI toolkit. It provides a material design desktop with
frosted glass effects, spring-curve animations, tiling mode, virtual
desktops, trackpad gestures, and an IPC socket for scripting.

Includes: compositor, panel, app launcher, notification system,
system tray, screen locker, and tyde_wmctl CLI.

%prep
# Source is pre-built binaries + data files (see scripts/build-rpm.sh)

%install
rm -rf %{buildroot}

# Binaries
install -Dm0755 %{_sourcedir}/tyde_compositor          %{buildroot}%{_bindir}/tyde_compositor
install -Dm0755 %{_sourcedir}/tyde_panel       %{buildroot}%{_bindir}/tyde_panel
install -Dm0755 %{_sourcedir}/tyde_wmctl         %{buildroot}%{_bindir}/tyde_wmctl
install -Dm0755 %{_sourcedir}/tyde_emoji         %{buildroot}%{_bindir}/tyde_emoji
install -Dm0755 %{_sourcedir}/tyde_chooser       %{buildroot}%{_bindir}/tyde_chooser

# Session desktop file
install -Dm0644 %{_sourcedir}/tyde-wayland.desktop \
    %{buildroot}%{_datadir}/wayland-sessions/tyde-wayland.desktop

# Portal definition
install -Dm0644 %{_sourcedir}/tyde.portal \
    %{buildroot}%{_datadir}/xdg-desktop-portal/portals/tyde.portal

# Man pages
install -Dm0644 %{_sourcedir}/tyde_compositor.1 %{buildroot}%{_mandir}/man1/tyde_compositor.1
install -Dm0644 %{_sourcedir}/tyde_panel.1      %{buildroot}%{_mandir}/man1/tyde_panel.1
install -Dm0644 %{_sourcedir}/tyde_wmctl.1        %{buildroot}%{_mandir}/man1/tyde_wmctl.1
install -Dm0644 %{_sourcedir}/tyde.5            %{buildroot}%{_mandir}/man5/tyde.5

# Documentation
install -d %{buildroot}%{_docdir}/%{name}
for doc in install.md keybindings.md configuration.md; do
    [ -f "%{_sourcedir}/$doc" ] && install -m0644 "%{_sourcedir}/$doc" %{buildroot}%{_docdir}/%{name}/
done

%files
%{_bindir}/tyde_compositor
%{_bindir}/tyde_panel
%{_bindir}/tyde_wmctl
%{_bindir}/tyde_emoji
%{_bindir}/tyde_chooser
%{_datadir}/wayland-sessions/tyde-wayland.desktop
%{_datadir}/xdg-desktop-portal/portals/tyde.portal
%{_mandir}/man1/tyde_compositor.1*
%{_mandir}/man1/tyde_panel.1*
%{_mandir}/man1/tyde_wmctl.1*
%{_mandir}/man5/tyde.5*
%{_docdir}/%{name}/

%changelog
* Wed Sep 23 2026 Tyde Contributors <tyde@fyshos.com> - 1.1.0-2
- Build against the distribution's wlroots 0.20 instead of a bundled 0.17

* Sun Mar 02 2026 Tyde Contributors <tyde@fyshos.com> - 1.1.0-1
- Initial RPM package
- Wayland compositor with wlroots 0.17
- Panel, app launcher, notification system, system tray
- Screen locker, tyde_wmctl CLI
- Flatpak security-context support (wp_security_context_v1)
- Adaptive sync / VRR / FreeSync support
- Virtual desktop pager, tiling mode
