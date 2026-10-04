package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
)

// fakeChatProvider is a scriptable llm.Provider for chat service tests.
type fakeChatProvider struct {
	mu          sync.Mutex
	streamFn    func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error
	summary     string
	chatErr     error
	streamCalls int
	chatCalls   int
}

func (f *fakeChatProvider) Complete(context.Context, llm.CompleteRequest) (llm.CompleteResponse, error) {
	return llm.CompleteResponse{}, llm.ErrUnavailable
}

func (f *fakeChatProvider) Stream(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
	f.mu.Lock()
	f.streamCalls++
	fn := f.streamFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req, onDelta)
	}
	onDelta(llm.StreamDelta{Content: "回答"})
	return nil
}

func (f *fakeChatProvider) ChatComplete(context.Context, llm.ChatCompletionRequest) (string, error) {
	f.mu.Lock()
	f.chatCalls++
	summary, cerr := f.summary, f.chatErr
	f.mu.Unlock()
	if cerr != nil {
		return "", cerr
	}
	return summary, nil
}

func (f *fakeChatProvider) Capabilities() llm.Capabilities {
	// ContextTokens unknown (0): effective limits fall back to the settings.
	return llm.Capabilities{SupportsThinking: true, SupportsStructuredOutput: true}
}

func (f *fakeChatProvider) counts() (streams, chats int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamCalls, f.chatCalls
}

// newChatTestService builds a service on a fresh SQLite database.
func newChatTestService(t *testing.T, provider llm.Provider) (*Service, *repository.ChatsRepo, *repository.SettingsRepo) {
	t.Helper()
	db := mustOpenDB(t)
	chatsRepo := repository.NewChatsRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	svc := NewService(chatsRepo, settingsRepo, llm.FixedResolver{P: provider}, func() string { return "chat-model" }, nil)
	return svc, chatsRepo, settingsRepo
}

// mustOpenDB opens a migrated throwaway database.
func mustOpenDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migration.Run(db, migration.Embedded()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func mustCreateChat(t *testing.T, svc *Service, title string) string {
	t.Helper()
	row, err := svc.CreateChat(context.Background(), CreateChatInput{Title: title})
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}
	return row.ID
}

func putSetting(t *testing.T, settings *repository.SettingsRepo, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal setting: %v", err)
	}
	if err := settings.Put(context.Background(), key, string(raw)); err != nil {
		t.Fatalf("put setting %s: %v", key, err)
	}
}

func drainEvents(events <-chan Event) []Event {
	var out []Event
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func TestServiceGeneratePersistsAndCompletes(t *testing.T) {
	provider := &fakeChatProvider{summary: "摘要"}
	svc, chatsRepo, _ := newChatTestService(t, provider)
	chatID := mustCreateChat(t, svc, "会话")

	genID, events, err := svc.Start(context.Background(), chatID, StartInput{Content: "问题"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(genID) != 26 {
		t.Errorf("generation id %q is not a ULID", genID)
	}
	evs := drainEvents(events)

	var started int
	var completed *CompletedData
	seqs := make([]int, 0, len(evs))
	for i, ev := range evs {
		seqs = append(seqs, ev.Seq)
		if i == 0 && ev.Name != EventStarted {
			t.Errorf("first event = %s, want generation.started", ev.Name)
		}
		switch ev.Name {
		case EventStarted:
			started++
			data := ev.Data.(StartedData)
			if data.ChatID != chatID || data.Model != "chat-model" || data.Thinking {
				t.Errorf("started data = %+v", data)
			}
		case EventContent:
			if _, ok := ev.Data.(DeltaData); !ok {
				t.Errorf("content delta data = %T", ev.Data)
			}
		case EventCompleted:
			c := ev.Data.(CompletedData)
			completed = &c
		default:
			t.Errorf("unexpected event %s", ev.Name)
		}
	}
	if started != 1 {
		t.Errorf("started events = %d, want 1", started)
	}
	for i, seq := range seqs {
		if seq != i {
			t.Fatalf("seq sequence = %v, want 0..n incrementing", seqs)
		}
	}
	if completed == nil {
		t.Fatalf("no completed event: %+v", evs)
	}
	if completed.FinishReason != "stop" || completed.Content != "回答" {
		t.Errorf("completed = %+v", completed)
	}

	messages, err := chatsRepo.ListMessages(context.Background(), chatID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[0].Content != "问题" {
		t.Fatalf("messages = %+v", messages)
	}
	last := messages[1]
	if last.Role != "assistant" || last.Content != "回答" || !last.GenerationID.Valid || last.GenerationID.String != genID {
		t.Errorf("assistant row = %+v (want generation_id %s)", last, genID)
	}
	if last.ID != completed.MessageID {
		t.Errorf("completed.message_id %q != persisted id %q", completed.MessageID, last.ID)
	}
}

func TestServiceSecondStartConflicts(t *testing.T) {
	release := make(chan struct{})
	provider := &fakeChatProvider{streamFn: func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		<-release
		return nil
	}}
	svc, _, _ := newChatTestService(t, provider)
	chatID := mustCreateChat(t, svc, "会话")

	_, events, err := svc.Start(context.Background(), chatID, StartInput{Content: "第一条"})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	_, _, err = svc.Start(context.Background(), chatID, StartInput{Content: "第二条"})
	if err == nil {
		t.Fatal("second start must conflict")
	}
	var appE interface{ Error() string }
	if !errors.As(err, &appE) || !strings.Contains(err.Error(), "GENERATION_ALREADY_ACTIVE") {
		t.Errorf("second start error = %v, want GENERATION_ALREADY_ACTIVE", err)
	}
	close(release)
	drainEvents(events)
}

func TestServiceStreamErrorEmitsErrorEventAndPersistsPartial(t *testing.T) {
	provider := &fakeChatProvider{streamFn: func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "部分"})
		return llm.ErrConnection
	}}
	svc, chatsRepo, _ := newChatTestService(t, provider)
	chatID := mustCreateChat(t, svc, "会话")

	_, events, err := svc.Start(context.Background(), chatID, StartInput{Content: "问题"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	evs := drainEvents(events)
	var errData *ErrorData
	var completed bool
	for _, ev := range evs {
		switch ev.Name {
		case EventError:
			e := ev.Data.(ErrorData)
			errData = &e
		case EventCompleted:
			completed = true
		}
	}
	if errData == nil || completed {
		t.Fatalf("events = %+v, want error without completed", evs)
	}
	if errData.Code != "PROVIDER_CONNECTION_FAILED" || !errData.Retryable {
		t.Errorf("error data = %+v", errData)
	}
	messages, _ := chatsRepo.ListMessages(context.Background(), chatID)
	if len(messages) != 2 || messages[1].Content != "部分" {
		t.Errorf("partial assistant row not persisted: %+v", messages)
	}
}

func TestServiceCancelPersistsPartialAndEmitsCancelled(t *testing.T) {
	ctx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	started := make(chan struct{})
	provider := &fakeChatProvider{streamFn: func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "部分"})
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	svc, chatsRepo, _ := newChatTestService(t, provider)
	chatID := mustCreateChat(t, svc, "会话")

	genID, events, err := svc.Start(ctx, chatID, StartInput{Content: "问题"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	<-started
	if err := svc.Cancel(chatID, genID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	evs := drainEvents(events)
	// Once the event channel closed the generation has settled and
	// unregistered: cancelling again must 404.
	if err := svc.Cancel(chatID, genID); err == nil ||
		!strings.Contains(err.Error(), "NOT_FOUND") {
		t.Errorf("second cancel = %v, want NOT_FOUND", err)
	}
	cancelled := false
	for _, ev := range evs {
		if ev.Name == EventCancelled {
			cancelled = true
			if _, ok := ev.Data.(CancelledData); !ok {
				t.Errorf("cancelled data = %T", ev.Data)
			}
		}
		if ev.Name == EventError {
			t.Errorf("cancel must not emit generation.error: %+v", evs)
		}
	}
	if !cancelled {
		t.Fatalf("no cancelled event: %+v", evs)
	}
	messages, _ := chatsRepo.ListMessages(context.Background(), chatID)
	if len(messages) != 2 || messages[1].Content != "部分" || messages[1].Role != "assistant" {
		t.Errorf("partial content not persisted: %+v", messages)
	}
}

func TestServiceCompactStoresSummary(t *testing.T) {
	provider := &fakeChatProvider{summary: "压缩后的摘要"}
	svc, chatsRepo, _ := newChatTestService(t, provider)
	chatID := mustCreateChat(t, svc, "会话")
	drainEvents(mustStart(t, svc, chatID, StartInput{Content: "历史消息"}))
	// The generation's auto-compact must not trigger (default threshold far).
	summary, err := svc.Compact(context.Background(), chatID, false)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if summary != "压缩后的摘要" {
		t.Errorf("summary = %q", summary)
	}
	row, err := chatsRepo.GetChat(context.Background(), chatID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if row.CompactSummary != "压缩后的摘要" {
		t.Errorf("stored compact summary = %q", row.CompactSummary)
	}
	if streams, _ := provider.counts(); streams != 1 {
		t.Errorf("stream calls = %d, want 1 (compact must not re-stream)", streams)
	}
}

func TestServiceAutoCompactTriggersAtThreshold(t *testing.T) {
	longContent := strings.Repeat("x", 400) // ~100 tokens ≫ 40*0.8
	provider := &fakeChatProvider{streamFn: func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: longContent})
		return nil
	}, summary: "自动摘要"}
	svc, chatsRepo, settingsRepo := newChatTestService(t, provider)
	putSetting(t, settingsRepo, "max_context_tokens", 40)
	putSetting(t, settingsRepo, "auto_compact", true)
	putSetting(t, settingsRepo, "compact_threshold", 0.8)
	chatID := mustCreateChat(t, svc, "会话")

	evs := drainEvents(mustStart(t, svc, chatID, StartInput{Content: longContent}))
	if evs[len(evs)-1].Name != EventCompleted {
		t.Fatalf("last event = %s, want completed", evs[len(evs)-1].Name)
	}
	// Deterministic: the channel closes only after auto compact finished.
	if _, chats := provider.counts(); chats != 1 {
		t.Errorf("ChatComplete calls = %d, want 1 (auto compact ran)", chats)
	}
	row, _ := chatsRepo.GetChat(context.Background(), chatID)
	if row.CompactSummary != "自动摘要" {
		t.Errorf("compact summary = %q, want auto-stored summary", row.CompactSummary)
	}
}

func TestServiceAutoCompactFailureDoesNotFailGeneration(t *testing.T) {
	longContent := strings.Repeat("x", 400)
	provider := &fakeChatProvider{streamFn: func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: longContent})
		return nil
	}, chatErr: llm.ErrUnavailable}
	svc, _, settingsRepo := newChatTestService(t, provider)
	putSetting(t, settingsRepo, "max_context_tokens", 40)
	putSetting(t, settingsRepo, "auto_compact", true)
	putSetting(t, settingsRepo, "compact_threshold", 0.8)
	chatID := mustCreateChat(t, svc, "会话")

	evs := drainEvents(mustStart(t, svc, chatID, StartInput{Content: longContent}))
	if evs[len(evs)-1].Name != EventCompleted {
		t.Fatalf("last event = %s, want completed despite compact failure", evs[len(evs)-1].Name)
	}
	for _, ev := range evs {
		if ev.Name == EventError {
			t.Errorf("compact failure must never emit generation.error: %+v", evs)
		}
	}
	if _, chats := provider.counts(); chats != 1 {
		t.Errorf("ChatComplete calls = %d, want 1 (attempted)", chats)
	}
}

func TestServiceNotConfiguredEmitsErrorEvent(t *testing.T) {
	db := mustOpenDB(t)
	chatsRepo := repository.NewChatsRepo(db)
	settingsRepo := repository.NewSettingsRepo(db)
	svc := NewService(chatsRepo, settingsRepo, notConfiguredResolver{}, nil, nil)
	chatID := mustCreateChat(t, svc, "会话")

	_, events, err := svc.Start(context.Background(), chatID, StartInput{Content: "问题"})
	if err != nil {
		t.Fatalf("start must stream (200 SSE), got error: %v", err)
	}
	evs := drainEvents(events)
	var started, errored bool
	for _, ev := range evs {
		switch ev.Name {
		case EventStarted:
			started = true
		case EventError:
			errored = true
			data := ev.Data.(ErrorData)
			if data.Code != "PROVIDER_NOT_CONFIGURED" || data.Retryable {
				t.Errorf("error data = %+v", data)
			}
		}
	}
	if !started || !errored {
		t.Fatalf("events = %+v, want started+error", evs)
	}
	messages, _ := chatsRepo.ListMessages(context.Background(), chatID)
	// The user message is persisted before the provider is resolved.
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Errorf("messages = %+v, want the persisted user message only", messages)
	}
}

func TestServiceRegenerateDeletesLastAssistant(t *testing.T) {
	var firstAssistantID string
	provider := &fakeChatProvider{streamFn: func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "旧回答"})
		return nil
	}}
	svc, chatsRepo, _ := newChatTestService(t, provider)
	chatID := mustCreateChat(t, svc, "会话")

	evs := drainEvents(mustStart(t, svc, chatID, StartInput{Content: "问题"}))
	for _, ev := range evs {
		if ev.Name == EventCompleted {
			firstAssistantID = ev.Data.(CompletedData).MessageID
		}
	}

	// Regenerate with a different answer to tell them apart.
	provider.mu.Lock()
	provider.streamFn = func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "新回答"})
		return nil
	}
	provider.mu.Unlock()

	evs = drainEvents(mustRegenerate(t, svc, chatID))
	if evs[len(evs)-1].Name != EventCompleted {
		t.Fatalf("last event = %s, want completed", evs[len(evs)-1].Name)
	}
	for _, ev := range evs {
		if ev.Name == EventStarted {
			if data := ev.Data.(StartedData); data.Thinking {
				t.Errorf("regenerate must use thinking=false: %+v", data)
			}
		}
	}
	messages, _ := chatsRepo.ListMessages(context.Background(), chatID)
	if len(messages) != 2 {
		t.Fatalf("messages after regenerate = %d, want 2 (user + ONE assistant): %+v", len(messages), messages)
	}
	if messages[1].ID == firstAssistantID || messages[1].Content != "新回答" {
		t.Errorf("old assistant row not replaced: %+v (old id %s)", messages[1], firstAssistantID)
	}
	if messages[0].Content != "问题" || messages[0].Role != "user" {
		t.Errorf("user row must be untouched: %+v", messages[0])
	}

	// A stream failure right after the user row persisted leaves the chat
	// with a user message last → regenerate must answer 400 INVALID_REQUEST.
	provider.mu.Lock()
	provider.streamFn = func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		return llm.ErrConnection
	}
	provider.mu.Unlock()
	fresh := mustCreateChat(t, svc, "空会话")
	drainEvents(mustStart(t, svc, fresh, StartInput{Content: "只有提问"}))
	messages, _ = chatsRepo.ListMessages(context.Background(), fresh)
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("messages on fresh chat = %+v, want a single user row", messages)
	}
	if _, _, err := svc.Regenerate(context.Background(), fresh); err == nil ||
		!strings.Contains(err.Error(), "INVALID_REQUEST") {
		t.Errorf("regenerate with user last = %v, want INVALID_REQUEST", err)
	}
}

func TestServiceGlobalContextRoundTrip(t *testing.T) {
	provider := &fakeChatProvider{}
	svc, _, _ := newChatTestService(t, provider)
	svc.SetGlobalContext(context.Background(), "全局知识")
	if got := svc.GlobalContext(context.Background()); got != "全局知识" {
		t.Errorf("global context = %q", got)
	}
}

// --- helpers that keep the tests above terse ---

func mustStart(t *testing.T, svc *Service, chatID string, in StartInput) <-chan Event {
	t.Helper()
	_, events, err := svc.Start(context.Background(), chatID, in)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return events
}

func mustRegenerate(t *testing.T, svc *Service, chatID string) <-chan Event {
	t.Helper()
	_, events, err := svc.Regenerate(context.Background(), chatID)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	return events
}

type notConfiguredResolver struct{}

func (notConfiguredResolver) TranslationProvider(context.Context) (llm.Provider, error) {
	return nil, llm.ErrNotConfigured
}
