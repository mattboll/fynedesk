// Package google implements the Google Calendar provider.
//
// Two credential paths are supported:
//
//  1. GNOME Online Accounts (GOA) — preferred when available. We connect
//     to the org.gnome.OnlineAccounts D-Bus service and ask it for fresh
//     access tokens for accounts the user has already configured. This
//     means fynedesk does not need its own OAuth client_id/secret.
//
//  2. Direct OAuth loopback flow — used when GOA is unavailable. The user
//     must supply their own client_id (and client_secret stub).
package google

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"fyshos.com/fynedesk/internal/calendar"
)

const (
	goaService     = "org.gnome.OnlineAccounts"
	goaRootPath    = "/org/gnome/OnlineAccounts"
	goaAccountIface = "org.gnome.OnlineAccounts.Account"
	goaOAuth2Iface  = "org.gnome.OnlineAccounts.OAuth2Based"
)

// GOAAvailable reports whether the GOA D-Bus service is reachable on the
// session bus. Used by the UI/CLI to decide whether to expose the
// "Add account from GNOME" path or fall back to OAuth loopback.
func GOAAvailable() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()

	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return false
	}
	for _, n := range names {
		if n == goaService {
			return true
		}
	}
	// Service may be activatable but not yet started — query activatable
	// names too.
	var actNames []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&actNames); err == nil {
		for _, n := range actNames {
			if n == goaService {
				return true
			}
		}
	}
	return false
}

// ListGOAGoogleAccounts enumerates Google accounts registered in GOA on the
// local session and returns them as fynedesk Account records (Source=GOA).
// The returned accounts have their D-Bus object path stashed in Extra["path"]
// so we can call back to GOA later for token refresh.
func ListGOAGoogleAccounts(ctx context.Context) ([]calendar.Account, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("dbus session: %w", err)
	}
	defer conn.Close()

	objMgr := conn.Object(goaService, dbus.ObjectPath(goaRootPath))
	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := objMgr.CallWithContext(ctx,
		"org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&managed); err != nil {
		return nil, fmt.Errorf("GOA GetManagedObjects: %w", err)
	}

	var out []calendar.Account
	for path, ifaces := range managed {
		acctProps, ok := ifaces[goaAccountIface]
		if !ok {
			continue
		}
		if v, ok := acctProps["ProviderType"]; ok {
			if s, _ := v.Value().(string); s != "google" {
				continue
			}
		} else {
			continue
		}
		// Skip accounts where calendar access is disabled in GNOME Settings.
		if v, ok := acctProps["CalendarDisabled"]; ok {
			if b, _ := v.Value().(bool); b {
				continue
			}
		}
		identity, _ := acctProps["Identity"].Value().(string)
		presentation, _ := acctProps["PresentationIdentity"].Value().(string)
		display := presentation
		if display == "" {
			display = identity
		}
		acc := calendar.Account{
			ID:       string(path),
			Provider: "google",
			Source:   calendar.SourceGOA,
			Email:    identity,
			Display:  display,
			Extra: map[string]string{
				"path": string(path),
			},
		}
		out = append(out, acc)
	}
	return out, nil
}

// GOAToken asks GOA for a fresh OAuth2 access token for the given account
// (identified by its D-Bus object path stored in Extra["path"]). The
// returned token is short-lived (typically ~1h); GOA caches it and renews
// automatically using its own refresh_token.
func GOAToken(ctx context.Context, account calendar.Account) (string, time.Time, error) {
	if account.Source != calendar.SourceGOA {
		return "", time.Time{}, errors.New("not a GOA account")
	}
	path, ok := account.Extra["path"]
	if !ok || path == "" {
		return "", time.Time{}, errors.New("missing GOA path")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("dbus session: %w", err)
	}
	defer conn.Close()

	obj := conn.Object(goaService, dbus.ObjectPath(path))
	var token string
	var expiresIn int32
	if err := obj.CallWithContext(ctx,
		goaOAuth2Iface+".GetAccessToken", 0).Store(&token, &expiresIn); err != nil {
		// Make the typical failure modes more debuggable.
		if strings.Contains(err.Error(), "Method") && strings.Contains(err.Error(), "not implemented") {
			return "", time.Time{}, fmt.Errorf("GOA account %s does not expose OAuth2 (re-add in GNOME Settings)", account.Email)
		}
		return "", time.Time{}, fmt.Errorf("GOA GetAccessToken: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	return token, expiresAt, nil
}
