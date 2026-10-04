package proxy

import (
	"context"
	"net/http"
	"testing"
)

func TestValidateModes(t *testing.T) {
	valid := []struct{ mode, url string }{
		{"system", ""},
		{"none", ""},
		{"http", "http://127.0.0.1:8080"},
		{"https", "https://proxy.example.com"},
		{"socks5", "socks5://127.0.0.1:1080"},
		{"", ""}, // defaults to system
	}
	for _, c := range valid {
		if err := Validate(c.mode, c.url); err != nil {
			t.Errorf("Validate(%q, %q) = %v, want nil", c.mode, c.url, err)
		}
	}
	invalid := []struct{ mode, url string }{
		{"ftp", "ftp://x"},          // unknown mode
		{"HTTP", "http://x"},        // case-sensitive enum
		{"http", ""},                // URL required
		{"http", "socks5://x:1080"}, // scheme mismatch
		{"socks5", "http://x:8080"}, // scheme mismatch
		{"https", "not a url"},      // unparseable
		{"socks5", "socks5:// "},    // no host
	}
	for _, c := range invalid {
		if err := Validate(c.mode, c.url); err == nil {
			t.Errorf("Validate(%q, %q) = nil, want error", c.mode, c.url)
		}
	}
}

func TestConfigureTransport(t *testing.T) {
	t.Run("system honors environment", func(t *testing.T) {
		tr := ConfigureTransport(&http.Transport{}, Settings{Mode: "system"})
		if tr.Proxy == nil {
			t.Fatal("system mode must set http.ProxyFromEnvironment")
		}
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		if _, err := tr.Proxy(req); err != nil {
			t.Fatalf("proxy lookup: %v", err)
		}
	})

	t.Run("none is direct", func(t *testing.T) {
		tr := ConfigureTransport(&http.Transport{Proxy: http.ProxyFromEnvironment}, Settings{Mode: "none"})
		if tr.Proxy != nil {
			t.Fatal("none mode must clear the proxy func")
		}
	})

	t.Run("http proxy URL", func(t *testing.T) {
		tr := ConfigureTransport(&http.Transport{}, Settings{Mode: "http", URL: "http://127.0.0.1:8899"})
		if tr.Proxy == nil {
			t.Fatal("http mode must set a proxy func")
		}
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
		u, err := tr.Proxy(req)
		if err != nil {
			t.Fatalf("proxy lookup: %v", err)
		}
		if u == nil || u.Host != "127.0.0.1:8899" {
			t.Fatalf("proxy URL = %v", u)
		}
	})

	t.Run("socks5 via DialContext", func(t *testing.T) {
		tr := ConfigureTransport(&http.Transport{}, Settings{Mode: "socks5", URL: "socks5://127.0.0.1:1080"})
		if tr.Proxy != nil {
			t.Fatal("socks5 mode must not use the http proxy func")
		}
		if tr.DialContext == nil {
			t.Fatal("socks5 mode must install a DialContext dialer (golang.org/x/net/proxy)")
		}
		// The dialer is real: dialing an unroutable socks endpoint fails fast
		// instead of bypassing the proxy.
		conn, err := tr.DialContext(context.Background(), "tcp", "example.com:80")
		if err == nil {
			_ = conn.Close()
			t.Fatal("dial through a non-existent socks5 proxy must fail")
		}
	})

	t.Run("invalid config fails closed to direct", func(t *testing.T) {
		tr := ConfigureTransport(&http.Transport{Proxy: http.ProxyFromEnvironment}, Settings{Mode: "http", URL: ":::bad"})
		if tr.Proxy != nil {
			t.Fatal("unparseable URL must fail closed to a direct connection")
		}
	})
}

func TestClientValidates(t *testing.T) {
	if _, err := Client(nil, Settings{Mode: "socks5", URL: ""}); err == nil {
		t.Fatal("Client must reject an invalid configuration")
	}
	c, err := Client(nil, Settings{Mode: "socks5", URL: "socks5://127.0.0.1:1080"})
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatal("Client transport must carry the socks5 dialer")
	}
}

func TestValidMode(t *testing.T) {
	for _, m := range []string{"system", "none", "http", "https", "socks5"} {
		if !ValidMode(m) {
			t.Errorf("ValidMode(%q) = false", m)
		}
	}
	if ValidMode("SOCKS5") || ValidMode("direct") || ValidMode("") {
		t.Error("ValidMode must reject unknown modes")
	}
}
