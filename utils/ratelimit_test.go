package utils

import (
	"net/http"
	"testing"
)

func TestIPRateLimiterBurstThenDeny(t *testing.T) {
	l := NewIPRateLimiter(0, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d within burst must be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("request beyond burst must be denied")
	}
	if !l.Allow("b") {
		t.Fatal("another client must have its own bucket")
	}
}

func TestClientIP(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:4444"
	if got := ClientIP(r); got != "10.0.0.5" {
		t.Fatalf("RemoteAddr host expected, got %q", got)
	}
	r.Header.Set("X-Forwarded-For", " 203.0.113.9 , 10.0.0.1")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("first X-Forwarded-For entry expected, got %q", got)
	}
}
