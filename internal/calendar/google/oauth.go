package google

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/oauth2"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"fyshos.com/fynedesk/internal/calendar"
)

// googleEndpoint is the OAuth 2.0 endpoint for Google.
var googleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

// requiredScopes are the OAuth scopes we need: read-only calendar +
// the user's email so we can label the account.
var requiredScopes = []string{
	gcal.CalendarReadonlyScope,
	gcal.CalendarEventsReadonlyScope,
	"https://www.googleapis.com/auth/userinfo.email",
}

// OAuthConfig is the user-supplied OAuth client identifier. ClientSecret
// can be empty when the Google client is registered as a "Desktop app"
// type (PKCE-protected), but Google still expects the field — we send an
// empty string and rely on PKCE for security.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// AuthorizeOAuth runs an interactive OAuth authorization in the user's
// browser via a one-shot loopback server. On success it returns:
//   - the exchanged token (including refresh_token)
//   - the user's email (so the caller can populate Account.Email)
//
// This function blocks until the user completes the consent screen, the
// context is cancelled, or the timeout (5 min) elapses. The HTTP server is
// torn down before the function returns; nothing keeps running afterwards.
func AuthorizeOAuth(ctx context.Context, oauthCfg OAuthConfig, openBrowser func(string) error) (*oauth2.Token, string, error) {
	if oauthCfg.ClientID == "" {
		return nil, "", errors.New("OAuth client_id is required")
	}
	if openBrowser == nil {
		openBrowser = defaultOpenBrowser
	}

	// Bind a free port on localhost.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("loopback bind: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/cb", port)

	cfg := &oauth2.Config{
		ClientID:     oauthCfg.ClientID,
		ClientSecret: oauthCfg.ClientSecret,
		Endpoint:     googleEndpoint,
		RedirectURL:  redirectURI,
		Scopes:       requiredScopes,
	}

	state, err := randomURLSafe(16)
	if err != nil {
		return nil, "", err
	}
	verifier, err := randomURLSafe(48)
	if err != nil {
		return nil, "", err
	}
	challenge := pkceChallenge(verifier)

	authURL := cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"), // force a refresh_token
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	type cbResult struct {
		code string
		err  error
	}
	resCh := make(chan cbResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/cb", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errStr := q.Get("error"); errStr != "" {
			http.Error(w, "Authorization denied: "+errStr, http.StatusBadRequest)
			resCh <- cbResult{err: fmt.Errorf("authorization denied: %s", errStr)}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			resCh <- cbResult{err: errors.New("OAuth state mismatch")}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing code", http.StatusBadRequest)
			resCh <- cbResult{err: errors.New("missing authorization code")}
			return
		}
		// Show a friendly closing page so the user knows they can return
		// to fynedesk. Inline HTML — keeping it small avoids any external
		// fetches that could leak the redirect URL.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8">
<title>FyneDesk · Calendar connected</title>
<style>body{font-family:system-ui,sans-serif;margin:6em auto;max-width:32em;text-align:center}</style>
<h1>FyneDesk Calendar</h1><p>Account connected. You can close this window.</p>`))
		resCh <- cbResult{code: code}
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go srv.Serve(listener)
	defer srv.Close()

	if err := openBrowser(authURL); err != nil {
		return nil, "", fmt.Errorf("open browser: %w", err)
	}

	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()

	var code string
	select {
	case res := <-resCh:
		if res.err != nil {
			return nil, "", res.err
		}
		code = res.code
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-timeout.C:
		return nil, "", errors.New("OAuth timed out — no callback received")
	}

	tok, err := cfg.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, "", fmt.Errorf("token exchange: %w", err)
	}

	email, err := fetchUserEmail(ctx, tok)
	if err != nil {
		// Non-fatal: we have a working token, the email is just nice
		// for labelling. Caller can fall back to "google account".
		email = ""
	}
	return tok, email, nil
}

// OAuthConfigFor returns an *oauth2.Config that the calendar API client
// can use to refresh access tokens for an account stored in our secret
// store. The returned config carries the user-supplied client_id/secret;
// callers feed the persisted refresh_token into config.TokenSource().
func OAuthConfigFor(secret calendar.SecretBundle) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     secret.ClientID,
		ClientSecret: secret.ClientSecret,
		Endpoint:     googleEndpoint,
		Scopes:       requiredScopes,
	}
}

// OAuthToken returns a fresh access token for an OAuth-flow account.
// It is a TokenFunc compatible with Provider.TokenFunc.
func OAuthToken(ctx context.Context, account calendar.Account) (string, time.Time, error) {
	if account.Source != calendar.SourceOAuth {
		return "", time.Time{}, errors.New("not an OAuth account")
	}
	secret, err := calendar.LoadSecret(account.ID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("load secret for %s: %w", account.ID, err)
	}
	if secret.RefreshToken == "" {
		return "", time.Time{}, errors.New("no refresh token (re-authorize the account)")
	}
	cfg := OAuthConfigFor(secret)
	src := cfg.TokenSource(ctx, &oauth2.Token{
		RefreshToken: secret.RefreshToken,
	})
	tok, err := src.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("token refresh: %w", err)
	}
	// Google sometimes rotates refresh_tokens; persist the new one.
	if tok.RefreshToken != "" && tok.RefreshToken != secret.RefreshToken {
		secret.RefreshToken = tok.RefreshToken
		_ = calendar.SaveSecret(account.ID, secret)
	}
	expiry := tok.Expiry
	if expiry.IsZero() {
		expiry = time.Now().Add(50 * time.Minute) // be safe
	}
	return tok.AccessToken, expiry, nil
}

// fetchUserEmail calls the userinfo endpoint to learn the email tied to
// the new token. We hit `https://openidconnect.googleapis.com/v1/userinfo`
// because it works with our minimal scope set.
func fetchUserEmail(ctx context.Context, tok *oauth2.Token) (string, error) {
	src := oauth2.StaticTokenSource(tok)
	svc, err := gcal.NewService(ctx, option.WithTokenSource(src))
	if err != nil {
		return "", err
	}
	// CalendarList.Get("primary") returns a CalendarListEntry where Id is
	// the primary calendar's ID — for a personal Google account that's
	// the email address. This avoids pulling the people/oauth2 SDK.
	entry, err := svc.CalendarList.Get("primary").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if strings.Contains(entry.Id, "@") {
		return entry.Id, nil
	}
	return "", nil
}

// pkceChallenge returns the base64-url-encoded SHA-256 of the verifier,
// which is what Google expects for PKCE S256.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomURLSafe(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// defaultOpenBrowser tries xdg-open, falling back to printing the URL.
func defaultOpenBrowser(target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return err
	}
	cmd := exec.Command("xdg-open", parsed.String())
	if err := cmd.Start(); err != nil {
		fmt.Printf("\nOpen this URL in your browser to authorize:\n  %s\n\n", parsed.String())
		return nil
	}
	go cmd.Wait()
	return nil
}
