package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Weber-5/FloatTranslate/backend/internal/chat"
	"github.com/Weber-5/FloatTranslate/backend/internal/config"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/terminology"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

// Server holds every dependency the handlers need.
type Server struct {
	cfg              config.Config
	logger           *slog.Logger
	pipeline         *translation.Pipeline
	settings         *repository.SettingsRepo
	history          *repository.HistoryRepo
	tabs             *repository.TabsRepo
	vocabulary       *repository.VocabularyRepo
	terms            *terminology.Service
	chats            *repository.ChatsRepo
	chatSvc          *chat.Service
	resolver         llm.Resolver
	providerSettings *ProviderSettingsStore
	ping             func() error
}

// NewServer builds the API server. providerSettings holds the provider
// configuration + in-memory API key; it MUST be the same instance the
// pipeline and the chat service were built with so a saved key is visible
// to both flows. resolver supplies the provider per request (the
// providerSettings store in production wiring; the mock only as a test
// fixture). ping reports database health for /health.
func NewServer(cfg config.Config, logger *slog.Logger, pipeline *translation.Pipeline,
	settings *repository.SettingsRepo, history *repository.HistoryRepo, tabs *repository.TabsRepo,
	vocabulary *repository.VocabularyRepo, terms *terminology.Service,
	chats *repository.ChatsRepo, chatSvc *chat.Service,
	providerSettings *ProviderSettingsStore, resolver llm.Resolver, ping func() error) *Server {
	return &Server{
		cfg:              cfg,
		logger:           logger,
		pipeline:         pipeline,
		settings:         settings,
		history:          history,
		tabs:             tabs,
		vocabulary:       vocabulary,
		terms:            terms,
		chats:            chats,
		chatSvc:          chatSvc,
		resolver:         resolver,
		providerSettings: providerSettings,
		ping:             ping,
	}
}

// Handler builds the fully wired chi router:
//   - /health is public (no auth)
//   - everything under /api/v1 requires the local session bearer token
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(CORS)
	r.Use(RequestLog(s.logger))
	r.Use(Recover(s.logger))

	r.Get("/health", s.Health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(Auth(s.cfg.SessionToken))

		r.Get("/runtime/capabilities", s.RuntimeCapabilities)

		r.Get("/settings", s.GetSettings)
		r.Put("/settings", s.PutSettings)
		r.Get("/settings/provider", s.GetProviderSettings)
		r.Put("/settings/provider", s.PutProviderSettings)
		r.Post("/settings/provider/test", s.TestProviderConnection)

		r.Post("/translations", s.CreateTranslation)
		r.Get("/translations/{id}", s.GetTranslation)
		r.Post("/translations/{id}/retranslate", s.Retranslate)

		r.Get("/history", s.ListHistory)
		r.Delete("/history", s.ClearHistory)
		r.Delete("/history/{id}", s.DeleteHistoryItem)

		r.Get("/vocabulary", s.ListVocabulary)
		r.Put("/vocabulary/{lemma}", s.SaveVocabulary)
		r.Delete("/vocabulary/{lemma}", s.DeleteVocabulary)

		r.Get("/terminology", s.ListTerminology)
		r.Post("/terminology", s.CreateTerminology)
		r.Put("/terminology/{id}", s.UpdateTerminology)
		r.Delete("/terminology/{id}", s.DeleteTerminology)

		r.Get("/tabs", s.GetTabs)
		r.Put("/tabs", s.PutTabs)

		// --- Phase 4: AI sidebar (chats, SSE generations, contexts) ---
		r.Get("/chats", s.ListChats)
		r.Post("/chats", s.CreateChat)
		r.Patch("/chats/{id}", s.UpdateChat)
		r.Delete("/chats/{id}", s.DeleteChat)

		r.Get("/chats/{id}/messages", s.ListChatMessages)
		r.Delete("/chats/{id}/messages", s.ClearChatMessages)

		r.Get("/chats/{id}/context", s.GetConversationContext)
		r.Put("/chats/{id}/context", s.PutConversationContext)

		r.Post("/chats/{id}/compact", s.CompactChat)
		r.Post("/chats/{id}/generations", s.CreateGeneration)
		r.Post("/chats/{id}/regenerate", s.RegenerateChat)
		r.Post("/chats/{id}/generations/{generationId}/cancel", s.CancelGeneration)

		r.Get("/context/global", s.GetGlobalContext)
		r.Put("/context/global", s.PutGlobalContext)
	})

	return r
}
