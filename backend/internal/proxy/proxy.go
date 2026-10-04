// Package proxy maps the frozen proxy settings (docs/00 §9, docs/08 §8) onto
// provider HTTP transports:
//
//	system → http.ProxyFromEnvironment (legacy default, still accepted)
//	none   → direct connection (no proxy)
//	http / https / socks5 → the URL from proxy_url
//
// An invalid mode/URL combination must be rejected at settings-save time
// (INVALID_REQUEST), never discovered on the request path.
package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

// Proxy modes (settings key proxy_mode).
const (
	ModeSystem = "system"
	ModeNone   = "none"
	ModeHTTP   = "http"
	ModeHTTPS  = "https"
	ModeSOCKS5 = "socks5"
)

// Settings are the two proxy-related settings rows.
type Settings struct {
	Mode string // "" = ModeSystem
	URL  string
}

// EffectiveMode returns the mode with its default applied.
func (s Settings) EffectiveMode() string {
	if strings.TrimSpace(s.Mode) == "" {
		return ModeSystem
	}
	return strings.TrimSpace(s.Mode)
}

// ValidMode reports whether mode is one of the five frozen modes.
func ValidMode(mode string) bool {
	switch mode {
	case ModeSystem, ModeNone, ModeHTTP, ModeHTTPS, ModeSOCKS5:
		return true
	}
	return false
}

// Validate checks a mode/URL pair. An empty mode defaults to system; URL is
// required and scheme-checked for the explicit proxy modes.
func Validate(mode, rawURL string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = ModeSystem
	}
	if !ValidMode(mode) {
		return fmt.Errorf("proxy_mode 仅支持 system、none、http、https 或 socks5")
	}
	switch mode {
	case ModeSystem, ModeNone:
		return nil
	}
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("启用自定义代理时 proxy_url 不能为空")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("proxy_url 无法解析: %v", err)
	}
	if u.Scheme != mode || u.Host == "" {
		return fmt.Errorf("proxy_url 必须是 %s://host:port 形式", mode)
	}
	return nil
}

// ConfigureTransport applies the settings to a cloned *http.Transport and
// returns it. The caller owns cloning; the passed transport is mutated.
// An unparseable configuration (impossible after Validate at save time)
// falls back to a direct connection.
func ConfigureTransport(tr *http.Transport, s Settings) *http.Transport {
	mode := s.EffectiveMode()
	switch mode {
	case ModeNone:
		tr.Proxy = nil
	case ModeHTTP, ModeHTTPS, ModeSOCKS5:
		u, err := url.Parse(strings.TrimSpace(s.URL))
		if err != nil || u.Scheme == "" {
			// Save-time validation makes this unreachable; fail closed to direct.
			tr.Proxy = nil
			return tr
		}
		if mode == ModeSOCKS5 {
			// http.Transport cannot dial socks5 natively here: use
			// golang.org/x/net/proxy.FromURL as the DialContext.
			dialer, derr := proxy.FromURL(u, proxy.Direct)
			if derr != nil {
				tr.Proxy = nil
				return tr
			}
			if cd, ok := dialer.(proxy.ContextDialer); ok {
				tr.Proxy = nil
				tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return cd.DialContext(ctx, network, addr)
				}
			}
			return tr
		}
		tr.Proxy = http.ProxyURL(u)
	default: // system (also the legacy default)
		tr.Proxy = http.ProxyFromEnvironment
	}
	return tr
}

// Client returns a copy of base (or a fresh client when base is nil) whose
// transport honors the settings. timeoutOverride < 0 keeps base's timeout
// untouched; use it for streaming clients that must not inherit an overall
// timeout.
func Client(base *http.Client, s Settings) (*http.Client, error) {
	mode := s.EffectiveMode()
	if err := Validate(mode, s.URL); err != nil {
		return nil, err
	}
	var tr *http.Transport
	if base != nil && base.Transport != nil {
		if baseTr, ok := base.Transport.(*http.Transport); ok {
			tr = baseTr.Clone()
		}
	}
	if tr == nil {
		def, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			tr = &http.Transport{}
		} else {
			tr = def.Clone()
		}
	}
	out := &http.Client{Transport: ConfigureTransport(tr, s)}
	if base != nil {
		out.Timeout = base.Timeout
		out.CheckRedirect = base.CheckRedirect
		out.Jar = base.Jar
	}
	return out, nil
}
