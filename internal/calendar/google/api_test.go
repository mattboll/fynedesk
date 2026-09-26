package google

import (
	"context"
	"testing"
	"time"

	"fyshos.com/tyde/internal/calendar"
)

func TestServiceIsKeptWhileTheTokenLasts(t *testing.T) {
	calls := 0
	p := &Provider{TokenFunc: func(context.Context, calendar.Account) (string, time.Time, error) {
		calls++
		return "tok", time.Now().Add(time.Hour), nil
	}}
	acc := calendar.Account{ID: "a"}
	s1, err := p.service(context.Background(), acc)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := p.service(context.Background(), acc)
	if calls != 1 || s1 != s2 {
		t.Fatalf("token asked %d times, same client: %v", calls, s1 == s2)
	}
	p.Forget("a")
	if _, _ = p.service(context.Background(), acc); calls != 2 {
		t.Errorf("after Forget: token asked %d times", calls)
	}
}

func TestServiceRenewsAnExpiringToken(t *testing.T) {
	calls := 0
	p := &Provider{TokenFunc: func(context.Context, calendar.Account) (string, time.Time, error) {
		calls++
		return "tok", time.Now().Add(30 * time.Second), nil // within the margin
	}}
	acc := calendar.Account{ID: "a"}
	_, _ = p.service(context.Background(), acc)
	_, _ = p.service(context.Background(), acc)
	if calls != 2 {
		t.Errorf("a token about to expire was reused (%d calls)", calls)
	}
}
