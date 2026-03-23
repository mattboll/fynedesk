%global goipath     fyshos.com/fynedesk
%global forgeurl    https://github.com/nicholasgasior/fynedesk

Name:           fynedesk
Version:        1.1.0
Release:        1%{?dist}
Summary:        Lightweight Wayland desktop environment written in Go

License:        BSD-3-Clause
URL:            %{forgeurl}
Source0:        %{name}-%{version}.tar.gz

# Bundled wlroots 0.17 shared library (not available in Fedora repos)
%global _privatelibs libwlroots
%global __provides_exclude ^(%{_privatelibs})\\.so
%global __requires_exclude ^(%{_privatelibs})\\.so

BuildRequires:  golang >= 1.21
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
FyneDesk is a full-featured Wayland compositor built with wlroots 0.17
and the Fyne GUI toolkit. It provides a material design desktop with
frosted glass effects, spring-curve animations, tiling mode, virtual
desktops, trackpad gestures, and an IPC socket for scripting.

Includes: compositor, panel, app launcher, notification system,
system tray, screen locker, and fynedesk-ctl CLI.

%prep
# Source is pre-built binaries + data files (see scripts/build-rpm.sh)

%install
rm -rf %{buildroot}

# Binaries
install -Dm0755 %{_sourcedir}/compositor          %{buildroot}%{_bindir}/fynedesk-compositor
install -Dm0755 %{_sourcedir}/fynedesk-panel       %{buildroot}%{_bindir}/fynedesk-panel
install -Dm0755 %{_sourcedir}/fynedesk-ctl         %{buildroot}%{_bindir}/fynedesk-ctl

# Bundled wlroots
install -d %{buildroot}%{_libdir}
for lib in %{_sourcedir}/wlroots-libs/libwlroots*.so*; do
    [ -f "$lib" ] && install -m0644 "$lib" %{buildroot}%{_libdir}/
done

# Session desktop file
install -Dm0644 %{_sourcedir}/fynedesk-wayland.desktop \
    %{buildroot}%{_datadir}/wayland-sessions/fynedesk-wayland.desktop

# Portal definition
install -Dm0644 %{_sourcedir}/fynedesk.portal \
    %{buildroot}%{_datadir}/xdg-desktop-portal/portals/fynedesk.portal

# Man pages
install -Dm0644 %{_sourcedir}/fynedesk-compositor.1 %{buildroot}%{_mandir}/man1/fynedesk-compositor.1
install -Dm0644 %{_sourcedir}/fynedesk-panel.1      %{buildroot}%{_mandir}/man1/fynedesk-panel.1
install -Dm0644 %{_sourcedir}/fynedesk-ctl.1        %{buildroot}%{_mandir}/man1/fynedesk-ctl.1
install -Dm0644 %{_sourcedir}/fynedesk.5            %{buildroot}%{_mandir}/man5/fynedesk.5

# Documentation
install -d %{buildroot}%{_docdir}/%{name}
for doc in install.md keybindings.md configuration.md; do
    [ -f "%{_sourcedir}/$doc" ] && install -m0644 "%{_sourcedir}/$doc" %{buildroot}%{_docdir}/%{name}/
done

%post
/sbin/ldconfig

%postun
/sbin/ldconfig

%files
%{_bindir}/fynedesk-compositor
%{_bindir}/fynedesk-panel
%{_bindir}/fynedesk-ctl
%{_libdir}/libwlroots*.so*
%{_datadir}/wayland-sessions/fynedesk-wayland.desktop
%{_datadir}/xdg-desktop-portal/portals/fynedesk.portal
%{_mandir}/man1/fynedesk-compositor.1*
%{_mandir}/man1/fynedesk-panel.1*
%{_mandir}/man1/fynedesk-ctl.1*
%{_mandir}/man5/fynedesk.5*
%{_docdir}/%{name}/

%changelog
* Sun Mar 02 2026 FyneDesk Contributors <fynedesk@fyshos.com> - 1.1.0-1
- Initial RPM package
- Wayland compositor with wlroots 0.17
- Panel, app launcher, notification system, system tray
- Screen locker, fynedesk-ctl CLI
- Flatpak security-context support (wp_security_context_v1)
- Adaptive sync / VRR / FreeSync support
- Virtual desktop pager, tiling mode
