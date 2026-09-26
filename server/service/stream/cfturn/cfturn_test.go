package cfturn

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testResponse = `{
  "iceServers": [
    {"urls": ["stun:stun.cloudflare.com:3478", "stun:stun.cloudflare.com:53"]},
    {
      "urls": [
        "turn:turn.cloudflare.com:3478?transport=udp",
        "turn:turn.cloudflare.com:53?transport=udp",
        "turns:turn.cloudflare.com:443?transport=tcp"
      ],
      "username": "user",
      "credential": "cred"
    }
  ]
}`

type testServer struct {
	*httptest.Server
	requests atomic.Int32
	status   atomic.Int32
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	s := &testServer{}
	s.status.Store(http.StatusCreated)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)

		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/key%2Fid/credentials/generate-ice-servers" && r.URL.RawPath != "/key%2Fid/credentials/generate-ice-servers" {
			t.Errorf("path = %s (raw %s)", r.URL.Path, r.URL.RawPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("authorization = %q", got)
		}
		var body generateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TTL != 86400 {
			t.Errorf("body ttl = %d, err = %v", body.TTL, err)
		}

		status := int(s.status.Load())
		w.WriteHeader(status)
		if status == http.StatusCreated {
			_, _ = w.Write([]byte(testResponse))
		}
	}))
	t.Cleanup(s.Close)

	return s
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func TestGetReturnsFilteredTurnServers(t *testing.T) {
	server := newTestServer(t)
	c := &clock{now: time.Unix(1_700_000_000, 0)}
	p := newProvider(server.Client(), server.URL, c.Now)

	servers := p.get("key/id", "token")

	if len(servers) != 1 {
		t.Fatalf("len(servers) = %d, want 1 (stun-only entry skipped)", len(servers))
	}
	want := []string{
		"turn:turn.cloudflare.com:3478?transport=udp",
		"turns:turn.cloudflare.com:443?transport=tcp",
	}
	if len(servers[0].URLs) != len(want) {
		t.Fatalf("urls = %v, want %v", servers[0].URLs, want)
	}
	for i := range want {
		if servers[0].URLs[i] != want[i] {
			t.Fatalf("urls = %v, want %v", servers[0].URLs, want)
		}
	}
	if servers[0].Username != "user" || servers[0].Credential != "cred" {
		t.Fatalf("credentials = %q/%v", servers[0].Username, servers[0].Credential)
	}
}

func TestGetWithoutConfigurationDoesNotRequest(t *testing.T) {
	server := newTestServer(t)
	p := newProvider(server.Client(), server.URL, time.Now)

	if servers := p.get("", "token"); servers != nil {
		t.Fatalf("servers = %v, want nil", servers)
	}
	if servers := p.get("key/id", ""); servers != nil {
		t.Fatalf("servers = %v, want nil", servers)
	}
	if n := server.requests.Load(); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

func TestGetCachesUntilRemainingLifetimeIsShort(t *testing.T) {
	server := newTestServer(t)
	c := &clock{now: time.Unix(1_700_000_000, 0)}
	p := newProvider(server.Client(), server.URL, c.Now)

	p.get("key/id", "token")
	c.now = c.now.Add(credentialTTL - minRemainingTTL - time.Minute)
	p.get("key/id", "token")
	if n := server.requests.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1 while cached", n)
	}

	c.now = c.now.Add(2 * time.Minute)
	p.get("key/id", "token")
	if n := server.requests.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2 after refresh window", n)
	}
}

func TestGetFailureBacksOffAndKeepsValidCache(t *testing.T) {
	server := newTestServer(t)
	c := &clock{now: time.Unix(1_700_000_000, 0)}
	p := newProvider(server.Client(), server.URL, c.Now)

	p.get("key/id", "token")

	server.status.Store(http.StatusInternalServerError)
	c.now = c.now.Add(credentialTTL - time.Hour)
	if servers := p.get("key/id", "token"); len(servers) != 1 {
		t.Fatalf("servers = %v, want cached servers after failed refresh", servers)
	}

	p.get("key/id", "token")
	if n := server.requests.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2 during backoff", n)
	}

	c.now = c.now.Add(time.Hour)
	if servers := p.get("key/id", "token"); servers != nil {
		t.Fatalf("servers = %v, want nil after expiry", servers)
	}
	if n := server.requests.Load(); n != 3 {
		t.Fatalf("requests = %d, want 3 after backoff elapsed", n)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	server := newTestServer(t)
	p := newProvider(server.Client(), server.URL, time.Now)

	servers := p.get("key/id", "token")
	servers[0].URLs[0] = "modified"

	if again := p.get("key/id", "token"); again[0].URLs[0] == "modified" {
		t.Fatal("cached servers were modified through returned slice")
	}
}
