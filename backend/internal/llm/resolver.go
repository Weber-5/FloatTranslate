package llm

import "context"

// Resolver supplies the provider implementation for a business request based
// on the current (possibly hot-reloaded) provider settings. Business layers
// depend on this instead of a fixed Provider so that changing the provider
// configuration at runtime takes effect without a restart.
type Resolver interface {
	// TranslationProvider returns the provider to use for a translation
	// request, or the ErrNotConfigured sentinel when the provider settings
	// are incomplete (mode + base_url + models + api_key required).
	TranslationProvider(ctx context.Context) (Provider, error)
}

// FixedResolver always returns the same provider. It exists as the test
// fixture hook: the mock provider is injected through it in tests and is
// never reachable via the HTTP surface in production builds.
type FixedResolver struct {
	P Provider
}

// TranslationProvider implements Resolver.
func (r FixedResolver) TranslationProvider(context.Context) (Provider, error) {
	return r.P, nil
}

// NotConfiguredResolver always reports ErrNotConfigured (Phase 2 smoke:
// without a configured key POST /translations must fail with
// PROVIDER_NOT_CONFIGURED instead of falling back to the mock).
type NotConfiguredResolver struct{}

// TranslationProvider implements Resolver.
func (NotConfiguredResolver) TranslationProvider(context.Context) (Provider, error) {
	return nil, ErrNotConfigured
}
