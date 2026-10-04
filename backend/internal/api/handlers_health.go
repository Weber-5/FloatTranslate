package api

import (
	"net/http"

	"github.com/Weber-5/FloatTranslate/backend/internal/dto"
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
// the provider's capabilities (Phase 1 mock: 1,000,000 / 8192).
func (s *Server) RuntimeCapabilities(w http.ResponseWriter, r *http.Request) {
	const (
		configuredContext = 1_000_000
		configuredOutput  = 8192
	)
	caps := s.provider.Capabilities()
	effectiveContext := min(configuredContext, caps.ContextTokens)
	effectiveOutput := min(configuredOutput, caps.OutputTokens)
	writeJSON(w, http.StatusOK, dto.RuntimeCapabilities{
		ConfiguredContextTokens:  configuredContext,
		EffectiveContextTokens:   effectiveContext,
		ConfiguredOutputTokens:   configuredOutput,
		EffectiveOutputTokens:    effectiveOutput,
		SupportsThinking:         caps.SupportsThinking,
		SupportsStructuredOutput: caps.SupportsStructuredOutput,
	})
}
