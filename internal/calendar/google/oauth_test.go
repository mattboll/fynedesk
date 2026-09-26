package google

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestOAuthCallbackNeedsState checks that only the browser coming back with
// our state ends the flow: a stray request to the port does not.
func TestOAuthCallbackNeedsState(t *testing.T) {
	get := func(t *testing.T, u string) int {
		t.Helper()
		resp, err := http.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	browser := func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		cb, state := q.Get("redirect_uri"), q.Get("state")
		if scopes := q.Get("scope"); strings.Contains(scopes, "userinfo") {
			t.Errorf("superfluous scope asked: %s", scopes)
		}
		go func() {
			if code := get(t, cb+"?state=forged&code=x"); code != http.StatusBadRequest {
				t.Errorf("forged state: HTTP %d", code)
			}
			if code := get(t, cb+"?error=access_denied"); code != http.StatusBadRequest {
				t.Errorf("no state: HTTP %d", code)
			}
			time.Sleep(100 * time.Millisecond) // the flow must still be waiting
			get(t, cb+"?state="+url.QueryEscape(state)+"&error=access_denied")
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := AuthorizeOAuth(ctx, OAuthConfig{ClientID: "test"}, browser)
	if err == nil || !strings.Contains(err.Error(), "authorization denied") {
		t.Fatalf("the flow ended with %v, want the user's denial", err)
	}
}
