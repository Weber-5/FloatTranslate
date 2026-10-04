package api

import (
	"net/http"

	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
)

// Health implements GET /health — public, no auth.
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	status := http.StatusOK
	respStatus := "ready"
	if err := s.pingDB(r); err != nil {
		dbStatus = "error"
		status = http.StatusServiceUnavailable
		respStatus = "unavailable"
	}
	writeJSON(w, status, dto.HealthResponse{
		Status:   respStatus,
		Version:  s.cfg.Version,
		DBStatus: dbStatus,
	})
}

// pingDB reports database health for /health.
func (s *Server) pingDB(r *http.Request) error {
	if s.ping == nil {
		return nil
	}
	return s.ping()
}

// RuntimeCapabilities implements GET /api/v1/runtime/capabilities. The
// configured values are the user settings; effective values are clamped by
// the provider's capabilities when the provider knows them (unknown
// capability = no clamp). When no provider is configured the configured
// values are reported unchanged.
func (s *Server) RuntimeCapabilities(w http.ResponseWriter, r *http.Request) {
	const (
		configuredContext = 1_000_000
		configuredOutput  = 8192
	)
	var caps llm.Capabilities
	if provider, err := s.resolver.TranslationProvider(r.Context()); err == nil {
		caps = provider.Capabilities()
	}
	effectiveContext := configuredContext
	if caps.ContextTokens > 0 {
		effectiveContext = min(configuredContext, caps.ContextTokens)
	}
	effectiveOutput := configuredOutput
	if caps.OutputTokens > 0 {
		effectiveOutput = min(configuredOutput, caps.OutputTokens)
	}
	writeJSON(w, http.StatusOK, dto.RuntimeCapabilities{
		ConfiguredContextTokens:  configuredContext,
		EffectiveContextTokens:   effectiveContext,
		ConfiguredOutputTokens:   configuredOutput,
		EffectiveOutputTokens:    effectiveOutput,
		SupportsThinking:         caps.SupportsThinking,
		SupportsStructuredOutput: caps.SupportsStructuredOutput,
	})
}
