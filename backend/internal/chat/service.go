// Package chat implements the AI sidebar generation manager (Phase 4,
// docs/00 §7, docs/06 §9-12): composing provider messages
//
//	System Prompt → Global Context → Conversation Context →
//	Compact Summary → Recent Messages
//
// running streaming generations with an in-memory active-generation registry
// (one generation per chat), persisting user/assistant message rows,
// cancelling (explicit endpoint or client disconnect), manual /compact and
// threshold-triggered auto-compact. Raw messages are never deleted by a
// compact — the summary only replaces the "old messages context
// representation" (docs/06 §10).
package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/ulcid"
)

// Settings keys read by the chat service (docs/00 §7 defaults live in the
// settings handler; the same defaults are repeated here for direct reads).
const (
	settingsKeySystemPrompt     = "ai_system_prompt"
	settingsKeyGlobalContext    = "global_context"
	settingsKeyMaxContextTokens = "max_context_tokens"
	settingsKeyAutoCompact      = "auto_compact"
	settingsKeyCompactThreshold = "compact_threshold"
)

// Frozen composition/context defaults.
const (
	// DefaultMaxContextTokens is the configured max context when unset.
	DefaultMaxContextTokens = 1_000_000
	// DefaultCompactThreshold is the auto-compact trigger ratio.
	DefaultCompactThreshold = 0.8
	// DefaultChatModel is used when no model func is wired.
	DefaultChatModel = "deepseek-flash"
)

// DefaultSystemPrompt is the built-in chat system prompt used when the user
// has not configured ai_system_prompt: a helpful bilingual assistant that
// answers in Markdown (docs/00 §7).
const DefaultSystemPrompt = `你是 FloatTranslate 内置的 AI 助手，一名乐于助人的中英双语助手。
- 始终使用 Markdown 格式输出：代码使用带语言标注的围栏代码块，按需使用标题、列表与表格。
- 用户用英文提问时用英文回答，用中文提问时用中文回答；翻译请求给出准确、地道的译文。
- 回答力求准确、简洁、结构清晰；不确定的内容如实说明。`

// Context message labels prepended to the composed system-adjacent context
// messages so the model can tell the blocks apart.
const (
	globalContextLabel  = "以下是用户设置的全局上下文（Global Context），回答时请参考："
	chatContextLabel    = "以下是当前会话的会话上下文（Conversation Context），回答时请参考："
	compactSummaryLabel = "以下是本会话此前对话的摘要（Compact Summary），回答时请作为背景知识："
	referenceTextLabel  = "以下是用户在本次提问中引用的文本（Reference）："
	compactSystemPrompt = "你是对话摘要器。请把对话压缩成一份高密度的中文摘要，供后续对话作为背景知识使用，省略无意义的寒暄与噪声。"
	defaultTitle        = "新会话"
	persistedRoleUser   = "user"
	persistedRoleAssist = "assistant"
	finishReasonStop    = "stop"
	eventBuf            = 1024
)

// EventName is an SSE event name of the generation stream.
type EventName string

// Frozen SSE event names (docs/04 §11).
const (
	EventStarted   EventName = "generation.started"
	EventReasoning EventName = "reasoning.delta"
	EventContent   EventName = "content.delta"
	EventCompleted EventName = "generation.completed"
	EventError     EventName = "generation.error"
	EventCancelled EventName = "generation.cancelled"
)

// Event is one generation stream event: the service assigns the frozen
// envelope (generation_id, seq starting at 0 and incrementing) while the
// handler only frames the bytes.
type Event struct {
	Name EventName
	Seq  int
	Data any
}

// StartedData is the generation.started payload.
type StartedData struct {
	ChatID   string `json:"chat_id"`
	Model    string `json:"model"`
	Thinking bool   `json:"thinking"`
}

// DeltaData is the reasoning.delta / content.delta payload.
type DeltaData struct {
	Text string `json:"text"`
}

// CompletedData is the generation.completed payload.
type CompletedData struct {
	MessageID        string `json:"message_id"`
	ReasoningContent string `json:"reasoning_content"`
	Content          string `json:"content"`
	FinishReason     string `json:"finish_reason"`
}

// ErrorData is the generation.error payload.
type ErrorData struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// CancelledData is the generation.cancelled payload (frozen empty object).
type CancelledData struct{}

// StartInput carries one CreateGeneration request body.
type StartInput struct {
	// Content is the user message, stored verbatim.
	Content string
	// Thinking requests the provider reasoning mode (chat only).
	Thinking bool
	// ReferenceText, when non-empty, is injected as an extra context message
	// (Ask AI text reference); it is NOT part of the stored user message.
	ReferenceText string
}

// ComposeInput is the frozen message composition (docs/06 §9 order).
type ComposeInput struct {
	SystemPrompt   string
	GlobalContext  string
	ChatContext    string
	CompactSummary string
	ReferenceText  string
	// History is the ascending message list (non-system rows only).
	History []repository.MessageRow
}

// ComposeMessages builds the provider message list in the frozen order:
// [system prompt] + [global context] + [conversation context] + [compact
// summary] + [reference text] (each only when non-empty, each a system
// message) + [history rows ascending].
func ComposeMessages(in ComposeInput) []llm.ChatMessage {
	msgs := make([]llm.ChatMessage, 0, len(in.History)+5)
	msgs = append(msgs, llm.ChatMessage{Role: "system", Content: in.SystemPrompt})
	if g := strings.TrimSpace(in.GlobalContext); g != "" {
		msgs = append(msgs, llm.ChatMessage{Role: "system", Content: globalContextLabel + "\n\n" + g})
	}
	if c := strings.TrimSpace(in.ChatContext); c != "" {
		msgs = append(msgs, llm.ChatMessage{Role: "system", Content: chatContextLabel + "\n\n" + c})
	}
	if s := strings.TrimSpace(in.CompactSummary); s != "" {
		msgs = append(msgs, llm.ChatMessage{Role: "system", Content: compactSummaryLabel + "\n\n" + s})
	}
	if ref := strings.TrimSpace(in.ReferenceText); ref != "" {
		msgs = append(msgs, llm.ChatMessage{Role: "system", Content: referenceTextLabel + "\n\n" + ref})
	}
	for _, m := range in.History {
		if m.Role == persistedRoleUser || m.Role == persistedRoleAssist {
			msgs = append(msgs, llm.ChatMessage{Role: m.Role, Content: m.Content})
		}
	}
	return msgs
}

// EstimateTokens estimates the token count of s as utf8 chars/4 (frozen
// Phase 4 estimation rule).
func EstimateTokens(s string) int {
	return len([]rune(s)) / 4
}

// activeGen is one in-flight generation registered per chat.
type activeGen struct {
	id     string
	cancel context.CancelFunc
}

// Service is the chat generation manager.
type Service struct {
	chats    *repository.ChatsRepo
	settings *repository.SettingsRepo
	resolver llm.Resolver
	logger   *slog.Logger
	model    func() string
	now      func() time.Time

	mu     sync.Mutex
	active map[string]*activeGen // chat id → in-flight generation
}

// NewService builds the chat generation manager. model (optional) supplies
// the configured chat model per generation; logger may be nil.
func NewService(chats *repository.ChatsRepo, settings *repository.SettingsRepo,
	resolver llm.Resolver, model func() string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		chats:    chats,
		settings: settings,
		resolver: resolver,
		logger:   logger,
		model:    model,
		now:      time.Now,
		active:   make(map[string]*activeGen),
	}
}

// Model returns the configured chat model.
func (s *Service) Model() string {
	if s.model == nil {
		return DefaultChatModel
	}
	if m := strings.TrimSpace(s.model()); m != "" {
		return m
	}
	return DefaultChatModel
}

// Start runs one generation for chatID (docs/04 §11): persists the user
// message, registers the generation (one active generation per chat, else
// 409 GENERATION_ALREADY_ACTIVE), then streams events on the returned
// channel. The channel is closed when the generation settles; ctx comes
// from the HTTP request so a client disconnect aborts the provider call and
// is treated as a cancel (partial content is persisted).
func (s *Service) Start(ctx context.Context, chatID string, in StartInput) (string, <-chan Event, error) {
	chatRow, err := s.chats.GetChat(ctx, chatID)
	if err != nil {
		return "", nil, mapRepoError(err, "会话")
	}
	genID := ulcid.New()
	genCtx, cancel, err := s.register(ctx, chatID, genID)
	if err != nil {
		return "", nil, err
	}
	events := make(chan Event, eventBuf)
	go s.run(genCtx, cancel, chatRow, in, genID, events)
	return genID, events, nil
}

// Regenerate re-runs the generation pipeline for the chat's last assistant
// answer (docs/04 §11): the last message must be an assistant row (else 400
// INVALID_REQUEST); that row is deleted and the SAME pipeline streams a new
// answer from the remaining history — no new user message is persisted and
// thinking is always off in 1.0.
func (s *Service) Regenerate(ctx context.Context, chatID string) (string, <-chan Event, error) {
	chatRow, err := s.chats.GetChat(ctx, chatID)
	if err != nil {
		return "", nil, mapRepoError(err, "会话")
	}

	s.mu.Lock()
	if _, busy := s.active[chatID]; busy {
		s.mu.Unlock()
		return "", nil, apperr.New(apperr.CodeGenerationAlreadyActive, "当前会话已有进行中的生成", false)
	}
	messages, err := s.chats.ListMessages(ctx, chatID)
	if err != nil {
		s.mu.Unlock()
		return "", nil, mapRepoError(err, "消息")
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != persistedRoleAssist {
		s.mu.Unlock()
		return "", nil, apperr.New(apperr.CodeInvalidRequest, "最后一条消息不是助手回复，无法重新生成", false)
	}
	if err := s.chats.DeleteMessage(ctx, messages[len(messages)-1].ID); err != nil {
		s.mu.Unlock()
		return "", nil, mapRepoError(err, "消息")
	}
	genID := ulcid.New()
	genCtx, cancel := context.WithCancel(ctx)
	s.active[chatID] = &activeGen{id: genID, cancel: cancel}
	s.mu.Unlock()

	events := make(chan Event, eventBuf)
	// No new user message: content/reference empty, thinking off (frozen).
	go s.run(genCtx, cancel, chatRow, StartInput{}, genID, events)
	return genID, events, nil
}

// Cancel aborts the active generation with generationID for chatID. It
// reports 404 NOT_FOUND when no such generation is in flight; the run loop
// then persists the partial content and emits generation.cancelled.
func (s *Service) Cancel(chatID, generationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	gen := s.active[chatID]
	if gen == nil || gen.id != generationID {
		return apperr.New(apperr.CodeNotFound, "该会话没有进行中的此生成任务", false)
	}
	gen.cancel()
	return nil
}

// CancelActive aborts whatever generation is active for chatID (used when a
// chat is deleted mid-generation). It reports whether one was cancelled.
func (s *Service) CancelActive(chatID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen := s.active[chatID]; gen != nil {
		gen.cancel()
		return true
	}
	return false
}

// register creates and registers the generation context (a child of the
// HTTP request context, so a client disconnect cancels the provider call
// and is treated as a cancel); 409 when the chat already has an active
// generation.
func (s *Service) register(parent context.Context, chatID, generationID string) (context.Context, context.CancelFunc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.active[chatID]; busy {
		return nil, nil, apperr.New(apperr.CodeGenerationAlreadyActive, "当前会话已有进行中的生成", false)
	}
	ctx, cancel := context.WithCancel(parent)
	s.active[chatID] = &activeGen{id: generationID, cancel: cancel}
	return ctx, cancel, nil
}

// unregister removes the chat's active-generation entry. The generation
// stops counting as active the moment its stream settles — auto compact
// (which runs afterwards) and new generations must not see it as busy.
func (s *Service) unregister(chatID string) {
	s.mu.Lock()
	delete(s.active, chatID)
	s.mu.Unlock()
}

// run is the generation pipeline body. It always unregisters the generation
// (as soon as the stream settles) and closes the event channel when it
// returns.
func (s *Service) run(ctx context.Context, cancel context.CancelFunc, chatRow repository.ChatRow,
	in StartInput, genID string, events chan Event) {
	defer func() {
		s.unregister(chatRow.ID) // idempotent safety net for early returns
		cancel()
		close(events)
	}()

	seq := 0
	emit := func(name EventName, data any) {
		events <- Event{Name: name, Seq: seq, Data: data}
		seq++
	}

	nowStr := s.now().UTC().Format(time.RFC3339)
	// Persist the user message first (frozen: content verbatim; the Ask AI
	// reference_text is composed as context only). Regenerate sends neither.
	if strings.TrimSpace(in.Content) != "" || strings.TrimSpace(in.ReferenceText) != "" {
		err := s.chats.InsertMessage(ctx, repository.MessageRow{
			ID: ulcid.New(), ChatID: chatRow.ID, Role: persistedRoleUser,
			Content: in.Content, CreatedAt: nowStr,
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "chat: persist user message failed", slog.Any("error", err))
			emit(EventError, ErrorData{Code: string(apperr.CodeDatabaseError), Message: "写入用户消息失败", Retryable: true})
			return
		}
	}

	emit(EventStarted, StartedData{ChatID: chatRow.ID, Model: s.Model(), Thinking: in.Thinking})

	provider, err := s.resolver.TranslationProvider(ctx)
	if err != nil {
		emit(EventError, providerErrorData(err))
		return
	}

	history, err := s.chats.ListMessages(ctx, chatRow.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "chat: list messages failed", slog.Any("error", err))
		emit(EventError, ErrorData{Code: string(apperr.CodeDatabaseError), Message: "读取会话消息失败", Retryable: true})
		return
	}
	messages := ComposeMessages(ComposeInput{
		SystemPrompt:   s.systemPrompt(ctx),
		GlobalContext:  s.globalContext(ctx),
		ChatContext:    chatRow.ConversationContext,
		CompactSummary: chatRow.CompactSummary,
		ReferenceText:  in.ReferenceText,
		History:        history,
	})

	var content, reasoning strings.Builder
	streamErr := provider.Stream(ctx, llm.StreamRequest{
		Model:    s.Model(),
		Messages: messages,
		Thinking: in.Thinking,
	}, func(d llm.StreamDelta) {
		if d.Reasoning != "" {
			reasoning.WriteString(d.Reasoning)
			emit(EventReasoning, DeltaData{Text: d.Reasoning})
		}
		if d.Content != "" {
			content.WriteString(d.Content)
			emit(EventContent, DeltaData{Text: d.Content})
		}
	})

	// The stream has settled: the generation is no longer active. Persist,
	// final events and auto compact happen outside the registry so a new
	// generation (or compact) is never blocked by the finished one.
	s.unregister(chatRow.ID)

	switch {
	case streamErr == nil:
		messageID := s.persistAssistant(ctx, chatRow.ID, content.String(), reasoning.String(), genID, nowStr)
		emit(EventCompleted, CompletedData{
			MessageID:        messageID,
			ReasoningContent: reasoning.String(),
			Content:          content.String(),
			FinishReason:     finishReasonStop,
		})
		s.maybeAutoCompact(ctx, chatRow.ID, provider)
	case errors.Is(streamErr, context.Canceled):
		// Cancelled (endpoint or client disconnect): persist the partial
		// answer with a detached context (the generation context is already
		// canceled) and emit the frozen cancelled event.
		persistCtx := context.WithoutCancel(ctx)
		s.persistAssistantBestEffort(persistCtx, chatRow.ID, content.String(), reasoning.String(), genID, nowStr)
		emit(EventCancelled, CancelledData{})
		s.logger.InfoContext(ctx, "chat: generation cancelled",
			slog.String("chat_id", chatRow.ID), slog.String("generation_id", genID))
	default:
		emit(EventError, providerErrorData(streamErr))
		// Best-effort partial persist even on provider failure.
		persistCtx := context.WithoutCancel(ctx)
		s.persistAssistantBestEffort(persistCtx, chatRow.ID, content.String(), reasoning.String(), genID, nowStr)
	}
}

// persistAssistant stores the assistant row, stamps generation_id and bumps
// the chat's updated_at. Storage failures are logged (best-effort): the
// stream outcome is already fixed at this point.
func (s *Service) persistAssistant(ctx context.Context, chatID, content, reasoning, genID, nowStr string) string {
	messageID := ulcid.New()
	err := s.chats.InsertMessage(ctx, repository.MessageRow{
		ID: messageID, ChatID: chatID, Role: persistedRoleAssist,
		Content: content, ReasoningContent: reasoning, CreatedAt: nowStr,
		GenerationID: toNullString(genID),
	})
	if err == nil {
		err = s.chats.TouchChat(ctx, chatID, nowStr)
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "chat: persist assistant message failed", slog.Any("error", err))
	}
	return messageID
}

// persistAssistantBestEffort persists partial content on the cancel/error
// paths; nothing is written when neither content nor reasoning accumulated.
func (s *Service) persistAssistantBestEffort(ctx context.Context, chatID, content, reasoning, genID, nowStr string) {
	if content == "" && reasoning == "" {
		return
	}
	s.persistAssistant(ctx, chatID, content, reasoning, genID, nowStr)
}

// Compact runs the manual/auto compact routine (docs/06 §10): every current
// message is summarized by one non-streaming provider call into a dense
// Chinese summary (facts/definitions/decisions/open items/user preferences)
// which replaces chats.compact_summary; raw rows are kept. When the chat has
// active generation the manual entry point reports 409; auto (force=false)
// silently skips instead.
func (s *Service) Compact(ctx context.Context, chatID string, force bool) (string, error) {
	chatRow, err := s.chats.GetChat(ctx, chatID)
	if err != nil {
		return "", mapRepoError(err, "会话")
	}
	if !force {
		s.mu.Lock()
		_, busy := s.active[chatID]
		s.mu.Unlock()
		if busy {
			return "", apperr.New(apperr.CodeGenerationAlreadyActive, "当前会话已有进行中的生成", false)
		}
	}
	provider, err := s.resolver.TranslationProvider(ctx)
	if err != nil {
		return "", mapProviderError(err)
	}
	return s.runCompact(ctx, chatRow, provider)
}

// runCompact summarizes ALL current messages and stores the summary.
func (s *Service) runCompact(ctx context.Context, chatRow repository.ChatRow, provider llm.Provider) (string, error) {
	messages, err := s.chats.ListMessages(ctx, chatRow.ID)
	if err != nil {
		return "", mapRepoError(err, "消息")
	}
	if len(messages) == 0 {
		// Nothing to summarize: reset the summary.
		if err := s.chats.SetCompactSummary(ctx, chatRow.ID, "", s.now().UTC().Format(time.RFC3339)); err != nil {
			return "", mapRepoError(err, "会话")
		}
		return "", nil
	}

	transcript := buildTranscript(messages)
	summary, err := plainComplete(ctx, provider, s.Model(), compactSystemPrompt,
		"请将以下完整对话总结为一份高密度的中文摘要，必须覆盖：关键事实、定义、已做的决策、未完成事项、用户明确表达的偏好；省略无意义的对话噪声。直接输出摘要正文（Markdown），不要额外解释。\n\n<对话记录>\n"+transcript+"\n</对话记录>")
	if err != nil {
		return "", mapProviderError(err)
	}
	if err := s.chats.SetCompactSummary(ctx, chatRow.ID, summary, s.now().UTC().Format(time.RFC3339)); err != nil {
		return "", mapRepoError(err, "会话")
	}
	return summary, nil
}

// buildTranscript renders the ascending message list as a readable
// user/assistant transcript for the compact prompt.
func buildTranscript(messages []repository.MessageRow) string {
	var b strings.Builder
	for _, m := range messages {
		switch m.Role {
		case persistedRoleUser:
			b.WriteString("user: ")
		case persistedRoleAssist:
			b.WriteString("assistant: ")
		default:
			continue // system_internal rows never enter the transcript
		}
		b.WriteString(strings.TrimSpace(m.Content))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// plainComplete runs the non-streaming plain-text completion, preferring the
// PlainCompleter extension and falling back to a JSON-summary Complete call
// for providers that only implement the Phase 1 surface.
func plainComplete(ctx context.Context, provider llm.Provider, model, systemPrompt, userPrompt string) (string, error) {
	if pc, ok := provider.(llm.PlainCompleter); ok {
		return pc.ChatComplete(ctx, llm.ChatCompletionRequest{
			Model: model,
			Messages: []llm.ChatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
		})
	}
	resp, err := provider.Complete(ctx, llm.CompleteRequest{
		Model:        model,
		SystemPrompt: systemPrompt,
		Prompt:       userPrompt + "\n\n请以 JSON 对象 {\"summary\": \"…\"} 返回摘要。",
	})
	if err != nil {
		return "", err
	}
	var parsed struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal([]byte(resp.Content), &parsed) == nil && strings.TrimSpace(parsed.Summary) != "" {
		return strings.TrimSpace(parsed.Summary), nil
	}
	return strings.TrimSpace(resp.Content), nil
}

// maybeAutoCompact runs the threshold-triggered auto compact after a
// finished generation (docs/06 §10): estimate over contexts + all messages
// vs threshold × effective context tokens (provider capability when known,
// configured value otherwise). Every failure here is log-only — it can never
// fail the already-finished generation.
func (s *Service) maybeAutoCompact(ctx context.Context, chatID string, provider llm.Provider) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("chat: auto compact panicked", slog.Any("panic", rec))
		}
	}()
	auto, err := s.boolSetting(ctx, settingsKeyAutoCompact, true)
	if err != nil || !auto {
		return
	}
	// A new generation may have started while this one finished; never
	// compact under an active generation.
	s.mu.Lock()
	_, busy := s.active[chatID]
	s.mu.Unlock()
	if busy {
		return
	}

	threshold := s.floatSetting(ctx, settingsKeyCompactThreshold, DefaultCompactThreshold)
	configured := s.intSetting(ctx, settingsKeyMaxContextTokens, DefaultMaxContextTokens)
	effective := configured
	if caps := provider.Capabilities(); caps.ContextTokens > 0 {
		effective = min(configured, caps.ContextTokens)
	}
	estimate, err := s.estimateChatTokens(ctx, chatID)
	if err != nil {
		s.logger.WarnContext(ctx, "chat: auto compact estimate failed", slog.Any("error", err))
		return
	}
	if estimate <= int(float64(effective)*threshold) {
		return
	}
	if _, err := s.Compact(ctx, chatID, true); err != nil {
		// Log only: auto compact must never fail the finished generation.
		s.logger.WarnContext(ctx, "chat: auto compact failed", slog.Any("error", err))
	}
}

// estimateChatTokens estimates tokens over global context + conversation
// context + compact summary + every message content.
func (s *Service) estimateChatTokens(ctx context.Context, chatID string) (int, error) {
	chatRow, err := s.chats.GetChat(ctx, chatID)
	if err != nil {
		return 0, err
	}
	messages, err := s.chats.ListMessages(ctx, chatID)
	if err != nil {
		return 0, err
	}
	parts := make([]string, 0, len(messages)+3)
	parts = append(parts, s.globalContext(ctx), chatRow.ConversationContext, chatRow.CompactSummary)
	for _, m := range messages {
		parts = append(parts, m.Content)
	}
	return EstimateTokens(strings.Join(parts, "\n")), nil
}

// systemPrompt resolves the effective system prompt: the user's
// ai_system_prompt when set, the built-in default otherwise.
func (s *Service) systemPrompt(ctx context.Context) string {
	if v, err := s.stringSetting(ctx, settingsKeySystemPrompt); err == nil && strings.TrimSpace(v) != "" {
		return v
	}
	return DefaultSystemPrompt
}

// globalContext reads settings.global_context ("" when unset).
func (s *Service) globalContext(ctx context.Context) string {
	v, err := s.stringSetting(ctx, settingsKeyGlobalContext)
	if err != nil {
		return ""
	}
	return v
}

// --- typed settings reads (values are JSON-encoded in the settings table) ---

func (s *Service) stringSetting(ctx context.Context, key string) (string, error) {
	raw, err := s.settings.Get(ctx, key)
	if err != nil {
		return "", err
	}
	var v string
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", err
	}
	return v, nil
}

func (s *Service) boolSetting(ctx context.Context, key string, fallback bool) (bool, error) {
	raw, err := s.settings.Get(ctx, key)
	if err != nil {
		return fallback, err
	}
	var v bool
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return fallback, err
	}
	return v, nil
}

func (s *Service) floatSetting(ctx context.Context, key string, fallback float64) float64 {
	raw, err := s.settings.Get(ctx, key)
	if err != nil {
		return fallback
	}
	var v float64
	if err := json.Unmarshal([]byte(raw), &v); err != nil || v <= 0 {
		return fallback
	}
	return v
}

func (s *Service) intSetting(ctx context.Context, key string, fallback int) int {
	raw, err := s.settings.Get(ctx, key)
	if err != nil {
		return fallback
	}
	var v float64
	if err := json.Unmarshal([]byte(raw), &v); err != nil || v <= 0 {
		return fallback
	}
	return int(v)
}

// --- error mapping ---

func toNullString(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

// providerErrorData maps a provider/resolver failure to the frozen
// generation.error payload codes.
func providerErrorData(err error) ErrorData {
	switch {
	case errors.Is(err, llm.ErrNotConfigured):
		return ErrorData{Code: string(apperr.CodeProviderNotConfigured), Message: "尚未配置 AI 服务，请先在设置中完成 Provider 配置", Retryable: false}
	case errors.Is(err, llm.ErrConnection):
		return ErrorData{Code: string(apperr.CodeProviderConnectionFailed), Message: "无法连接 AI 服务，请检查网络或 Base URL 后重试", Retryable: true}
	case errors.Is(err, context.Canceled):
		// Should be handled by the cancel path; be defensive.
		return ErrorData{Code: string(apperr.CodeProviderUnavailable), Message: "生成已取消", Retryable: false}
	default:
		return ErrorData{Code: string(apperr.CodeProviderUnavailable), Message: "AI 服务暂时不可用，请稍后重试", Retryable: true}
	}
}

// mapProviderError maps provider sentinel errors to standard app errors.
func mapProviderError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, llm.ErrNotConfigured):
		return apperr.New(apperr.CodeProviderNotConfigured, "尚未配置 AI 服务，请先在设置中完成 Provider 配置", false)
	case errors.Is(err, llm.ErrConnection):
		return apperr.Wrap(apperr.CodeProviderConnectionFailed, "无法连接 AI 服务，请检查网络或 Base URL 后重试", true, err)
	default:
		return apperr.Wrap(apperr.CodeProviderUnavailable, "AI 服务暂时不可用，请稍后重试", true, err)
	}
}

// mapRepoError converts repository sentinel errors to standard app errors.
func mapRepoError(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return apperr.New(apperr.CodeNotFound, what+"不存在", false)
	case errors.Is(err, repository.ErrConflict):
		return apperr.New(apperr.CodeConflict, what+"已存在或冲突", false)
	default:
		return apperr.Wrap(apperr.CodeDatabaseError, "数据库操作失败", true, err)
	}
}

// CreateChatInput is the POST /chats body (title optional → default title).
type CreateChatInput struct {
	Title string
}

// CreateChat persists a new chat row and returns it.
func (s *Service) CreateChat(ctx context.Context, in CreateChatInput) (repository.ChatRow, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = defaultTitle
	}
	nowStr := s.now().UTC().Format(time.RFC3339)
	row := repository.ChatRow{ID: ulcid.New(), Title: title, CreatedAt: nowStr, UpdatedAt: nowStr}
	if err := s.chats.InsertChat(ctx, row); err != nil {
		return repository.ChatRow{}, mapRepoError(err, "会话")
	}
	return row, nil
}

// ListChats returns all chats, newest activity first.
func (s *Service) ListChats(ctx context.Context) ([]repository.ChatRow, error) {
	rows, err := s.chats.ListChats(ctx)
	if err != nil {
		return nil, mapRepoError(err, "会话")
	}
	return rows, nil
}

// RenameChat implements PATCH /chats/{id}.
func (s *Service) RenameChat(ctx context.Context, id, title string) (repository.ChatRow, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return repository.ChatRow{}, apperr.New(apperr.CodeInvalidRequest, "标题不能为空", false)
	}
	if err := s.chats.UpdateChatTitle(ctx, id, title, s.now().UTC().Format(time.RFC3339)); err != nil {
		return repository.ChatRow{}, mapRepoError(err, "会话")
	}
	row, err := s.chats.GetChat(ctx, id)
	if err != nil {
		return repository.ChatRow{}, mapRepoError(err, "会话")
	}
	return row, nil
}

// DeleteChat removes the chat (messages cascade) after aborting any active
// generation for it.
func (s *Service) DeleteChat(ctx context.Context, id string) error {
	s.CancelActive(id)
	if err := s.chats.DeleteChat(ctx, id); err != nil {
		return mapRepoError(err, "会话")
	}
	return nil
}

// ClearMessages implements DELETE /chats/{id}/messages (/clear): every
// message row and the compact summary are dropped, the conversation context
// is kept (docs/00 §7).
func (s *Service) ClearMessages(ctx context.Context, chatID string) error {
	if _, err := s.chats.GetChat(ctx, chatID); err != nil {
		return mapRepoError(err, "会话")
	}
	if err := s.chats.DeleteMessages(ctx, chatID); err != nil {
		return mapRepoError(err, "消息")
	}
	if err := s.chats.SetCompactSummary(ctx, chatID, "", s.now().UTC().Format(time.RFC3339)); err != nil {
		return mapRepoError(err, "会话")
	}
	return nil
}

// Messages returns the ascending message list of a chat.
func (s *Service) Messages(ctx context.Context, chatID string) ([]repository.MessageRow, error) {
	if _, err := s.chats.GetChat(ctx, chatID); err != nil {
		return nil, mapRepoError(err, "会话")
	}
	rows, err := s.chats.ListMessages(ctx, chatID)
	if err != nil {
		return nil, mapRepoError(err, "消息")
	}
	return rows, nil
}

// ConversationContext returns chats.conversation_context (404 when unknown).
func (s *Service) ConversationContext(ctx context.Context, chatID string) (string, error) {
	row, err := s.chats.GetChat(ctx, chatID)
	if err != nil {
		return "", mapRepoError(err, "会话")
	}
	return row.ConversationContext, nil
}

// SetConversationContext stores chats.conversation_context.
func (s *Service) SetConversationContext(ctx context.Context, chatID, content string) error {
	if err := s.chats.UpdateChatContext(ctx, chatID, content, s.now().UTC().Format(time.RFC3339)); err != nil {
		return mapRepoError(err, "会话")
	}
	return nil
}

// GlobalContext reads settings.global_context as plain text.
func (s *Service) GlobalContext(ctx context.Context) string {
	return s.globalContext(ctx)
}

// SetGlobalContext stores settings.global_context as plain text.
func (s *Service) SetGlobalContext(ctx context.Context, content string) error {
	raw, err := json.Marshal(content)
	if err != nil {
		return err
	}
	if err := s.settings.Put(ctx, settingsKeyGlobalContext, string(raw)); err != nil {
		return mapRepoError(err, "全局上下文")
	}
	return nil
}
