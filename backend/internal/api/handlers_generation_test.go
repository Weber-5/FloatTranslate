package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/chat"
	"github.com/Weber-5/FloatTranslate/backend/internal/llm"
)

// sseFrame is one parsed SSE event block.
type sseFrame struct {
	name string
	data string
}

// parseSSE validates the frozen framing (event line + one data line + blank
// line; comment lines allowed) and returns the event blocks.
func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		switch {
		case strings.HasPrefix(line, ":"):
			// heartbeat comment
		case line == "":
			// separator
		case strings.HasPrefix(line, "event: "):
			name := strings.TrimPrefix(line, "event: ")
			if i+1 >= len(lines) {
				t.Fatalf("event %q has no data line", name)
			}
			dataLine := strings.TrimSuffix(lines[i+1], "\r")
			if !strings.HasPrefix(dataLine, "data: ") {
				t.Fatalf("event %q not followed by a data line: %q", name, dataLine)
			}
			if i+2 >= len(lines) || strings.TrimSuffix(lines[i+2], "\r") != "" {
				t.Fatalf("event %q not terminated by a blank line", name)
			}
			frames = append(frames, sseFrame{name: name, data: strings.TrimPrefix(dataLine, "data: ")})
			i += 2
		default:
			t.Fatalf("unexpected SSE line: %q", line)
		}
	}
	return frames
}

// sseEnvelope mirrors the frozen per-event wrapper.
type sseEnvelope struct {
	GenerationID string          `json:"generation_id"`
	Seq          int             `json:"seq"`
	Data         json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, f sseFrame) sseEnvelope {
	t.Helper()
	var env sseEnvelope
	if err := json.Unmarshal([]byte(f.data), &env); err != nil {
		t.Fatalf("event %q data is not JSON: %v (%s)", f.name, err, f.data)
	}
	return env
}

// runGenerationToCompletion performs a recorder-based generation and returns
// (status, frames, raw body).
func runGenerationToCompletion(t *testing.T, h http.Handler, chatID, content string, thinking bool) (int, []sseFrame, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"content": content, "thinking": thinking})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID+"/generations", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, parseSSE(t, rec.Body.String()), rec.Body.String()
}

func TestGenerationSSEGoldenFraming(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	status, frames, body := runGenerationToCompletion(t, env.h, chatID, "hello", false)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s)", status, body)
	}
	wantNames := []string{"generation.started", "reasoning.delta", "content.delta", "content.delta", "generation.completed"}
	if len(frames) != len(wantNames) {
		t.Fatalf("frames = %d, want %d: %+v", len(frames), len(wantNames), frames)
	}
	var genID string
	for i, frame := range frames {
		if frame.name != wantNames[i] {
			t.Errorf("frames[%d].name = %q, want %q", i, frame.name, wantNames[i])
		}
		env := decodeEnvelope(t, frame)
		if i == 0 {
			genID = env.GenerationID
			if len(genID) != 26 {
				t.Errorf("generation_id %q is not a ULID", genID)
			}
		}
		if env.GenerationID != genID {
			t.Errorf("frames[%d] generation_id = %q, want constant %q", i, env.GenerationID, genID)
		}
		if env.Seq != i {
			t.Errorf("frames[%d].seq = %d, want %d", i, env.Seq, i)
		}
	}

	// Exact golden framing of the first frame (byte-for-byte, seq included).
	startedEnv := decodeEnvelope(t, frames[0])
	var started struct {
		ChatID   string `json:"chat_id"`
		Model    string `json:"model"`
		Thinking bool   `json:"thinking"`
	}
	if err := json.Unmarshal(startedEnv.Data, &started); err != nil {
		t.Fatalf("started data: %v", err)
	}
	if started.ChatID != chatID || started.Model != "deepseek-flash" || started.Thinking {
		t.Errorf("started data = %+v", started)
	}
	wantFirst := "event: generation.started\ndata: " + frames[0].data + "\n\n"
	if !strings.HasPrefix(body, wantFirst) {
		t.Errorf("first frame framing = %q, want prefix %q", body[:min(len(body), 200)], wantFirst)
	}

	completedEnv := decodeEnvelope(t, frames[len(frames)-1])
	var completed struct {
		MessageID        string `json:"message_id"`
		ReasoningContent string `json:"reasoning_content"`
		Content          string `json:"content"`
		FinishReason     string `json:"finish_reason"`
	}
	if err := json.Unmarshal(completedEnv.Data, &completed); err != nil {
		t.Fatalf("completed data: %v", err)
	}
	if completed.FinishReason != "stop" {
		t.Errorf("finish_reason = %q", completed.FinishReason)
	}
	if completed.Content != "（mock 回复）hello" {
		t.Errorf("content = %q", completed.Content)
	}
	if completed.ReasoningContent != "（mock 思考过程）" {
		t.Errorf("reasoning = %q", completed.ReasoningContent)
	}
	if len(completed.MessageID) != 26 {
		t.Errorf("message_id %q is not a ULID", completed.MessageID)
	}
	// Delta payloads carry {"text": ...}.
	reasoningDeltaEnv := decodeEnvelope(t, frames[1])
	var delta struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(reasoningDeltaEnv.Data, &delta); err != nil || delta.Text != "（mock 思考过程）" {
		t.Errorf("reasoning delta = %q err=%v", frames[1].data, err)
	}
	contentDeltaEnv := decodeEnvelope(t, frames[3])
	if err := json.Unmarshal(contentDeltaEnv.Data, &delta); err != nil || delta.Text != "hello" {
		t.Errorf("content delta = %q err=%v", frames[3].data, err)
	}
}

func TestGenerationValidationErrors(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	// Missing body / empty content → standard envelope, NOT an SSE stream.
	status, body := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"thinking": false})
	if status != http.StatusBadRequest {
		t.Fatalf("empty content = %d (%v), want 400", status, body)
	}
	if code, _ := errCode(t, body); code != "INVALID_REQUEST" {
		t.Errorf("code = %v", body)
	}
	status, body = do(t, env.h, http.MethodPost, "/api/v1/chats/UNKNOWN01/generations", testToken,
		map[string]any{"content": "hi", "thinking": false})
	if status != http.StatusNotFound {
		t.Errorf("unknown chat = %d, want 404", status)
	}
	if code, _ := errCode(t, body); code != "NOT_FOUND" {
		t.Errorf("unknown chat code = %v", body)
	}
}

func TestGenerationProviderNotConfiguredStreamsErrorEvent(t *testing.T) {
	env := newChatTestEnvWithResolver(t, llm.NotConfiguredResolver{})
	chatID := createChat(t, env.h, "")

	status, frames, body := runGenerationToCompletion(t, env.h, chatID, "hello", false)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 SSE (%s)", status, body)
	}
	var names []string
	var errData struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	}
	for _, f := range frames {
		names = append(names, f.name)
		if f.name == "generation.error" {
			e := decodeEnvelope(t, f)
			if err := json.Unmarshal(e.Data, &errData); err != nil {
				t.Fatalf("error data: %v (%s)", err, e.Data)
			}
		}
	}
	if len(names) != 2 || names[0] != "generation.started" || names[1] != "generation.error" {
		t.Fatalf("event names = %v", names)
	}
	if errData.Code != "PROVIDER_NOT_CONFIGURED" || errData.Retryable {
		t.Errorf("error data = %+v", errData)
	}
	// The user message is persisted before the provider is resolved.
	if rows := env.messageRows(t, chatID); len(rows) != 1 || rows[0][0] != "user" {
		t.Errorf("messages = %v, want only the user row", rows)
	}
}

func TestGenerationConflictWhenActive(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	env.mock.SetStreamFunc(func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		once.Do(func() { close(entered) })
		<-release
		onDelta(llm.StreamDelta{Content: "完成"})
		return nil
	})
	defer env.mock.SetStreamFunc(nil)

	srv := httptest.NewServer(env.h)
	defer srv.Close()

	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, http.MethodPost,
		srv.URL+"/api/v1/chats/"+chatID+"/generations", strings.NewReader(`{"content":"第一条","thinking":false}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first generation status = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(firstLine) != "event: generation.started" {
		t.Fatalf("first stream line = %q err=%v", firstLine, err)
	}
	<-entered // provider stream is now blocked in-flight

	// A second generation for the same chat must conflict; once the first
	// stream is released, another chat is unaffected.
	status, body := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/generations", testToken,
		map[string]any{"content": "第二条", "thinking": false})
	if status != http.StatusConflict {
		t.Fatalf("second generation = %d (%v), want 409", status, body)
	}
	if code, _ := errCode(t, body); code != "GENERATION_ALREADY_ACTIVE" {
		t.Errorf("conflict code = %v", body)
	}

	close(release)
	// Drain the first stream to completion.
	rest, _ := io.ReadAll(reader)
	frames := parseSSE(t, firstLine+string(rest))
	if frames[len(frames)-1].name != "generation.completed" {
		t.Errorf("last frame = %s, want completed", frames[len(frames)-1].name)
	}

	// The registry freed: another chat generates normally again.
	env.mock.SetStreamFunc(nil)
	other := createChat(t, env.h, "")
	if s, _, _ := runGenerationToCompletion(t, env.h, other, "别的会话", false); s != 200 {
		t.Errorf("generation on another chat = %d, want 200", s)
	}
}

func TestGenerationCancelEmitsCancelledAndPersistsPartial(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	entered := make(chan struct{})
	var once sync.Once
	env.mock.SetStreamFunc(func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "部分"})
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	})
	defer env.mock.SetStreamFunc(nil)

	srv := httptest.NewServer(env.h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost,
		srv.URL+"/api/v1/chats/"+chatID+"/generations", strings.NewReader(`{"content":"长问题","thinking":false}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generation: %v", err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if _, err := reader.ReadString('\n'); err != nil { // event line
		t.Fatalf("read started: %v", err)
	}
	startedData, _ := reader.ReadString('\n')
	var env0 sseEnvelope
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimRight(startedData, "\n"), "data: ")), &env0); err != nil {
		t.Fatalf("started envelope: %v (%q)", err, startedData)
	}
	<-entered

	// Cancel via the frozen endpoint.
	status, body := do(t, env.h, http.MethodPost,
		"/api/v1/chats/"+chatID+"/generations/"+env0.GenerationID+"/cancel", testToken, nil)
	if status != http.StatusAccepted {
		t.Fatalf("cancel = %d (%v), want 202", status, body)
	}
	// Unknown generation id → 404.
	status, body = do(t, env.h, http.MethodPost,
		"/api/v1/chats/"+chatID+"/generations/UNKNOWNGEN/cancel", testToken, nil)
	if status != http.StatusNotFound {
		t.Errorf("cancel unknown = %d (%v), want 404", status, body)
	}

	rest, _ := io.ReadAll(reader)
	frames := parseSSE(t, string(rest))
	if len(frames) == 0 || frames[len(frames)-1].name != "generation.cancelled" {
		t.Fatalf("tail frames = %+v, want generation.cancelled last", frames)
	}
	cancelEnv := decodeEnvelope(t, frames[len(frames)-1])
	if string(cancelEnv.Data) != "{}" {
		t.Errorf("cancelled data = %s, want {}", cancelEnv.Data)
	}
	for _, f := range frames {
		if f.name == "generation.completed" || f.name == "generation.error" {
			t.Errorf("cancel path must not emit %s", f.name)
		}
	}

	// The partial answer is persisted.
	if rows := env.messageRows(t, chatID); len(rows) != 2 || rows[1][0] != "assistant" || rows[1][1] != "部分" {
		t.Errorf("messages = %v, want persisted partial assistant row", rows)
	}
}

func TestGenerationMidStreamProviderError(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")
	env.mock.SetStreamFunc(func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "片段一"})
		onDelta(llm.StreamDelta{Content: "片段二"})
		return llm.ErrConnection
	})
	defer env.mock.SetStreamFunc(nil)

	status, frames, body := runGenerationToCompletion(t, env.h, chatID, "问题", false)
	if status != 200 {
		t.Fatalf("status = %d (%s)", status, body)
	}
	var names []string
	var errData struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	for _, f := range frames {
		names = append(names, f.name)
		if f.name == "generation.error" {
			e := decodeEnvelope(t, f)
			if err := json.Unmarshal(e.Data, &errData); err != nil {
				t.Fatalf("error data: %v", err)
			}
		}
	}
	want := []string{"generation.started", "content.delta", "content.delta", "generation.error"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", names, want)
	}
	if errData.Code != "PROVIDER_CONNECTION_FAILED" || !errData.Retryable {
		t.Errorf("error data = %+v", errData)
	}
	// Partial content is persisted best-effort.
	if rows := env.messageRows(t, chatID); len(rows) != 2 || rows[1][1] != "片段一片段二" {
		t.Errorf("messages = %v, want partial content persisted", rows)
	}
}

func TestGenerationRegenerate(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")
	_, frames, _ := runGenerationToCompletion(t, env.h, chatID, "hello", false)
	completedEnv := decodeEnvelope(t, frames[len(frames)-1])
	var completed struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(completedEnv.Data, &completed); err != nil {
		t.Fatalf("completed data: %v", err)
	}
	firstAssistant := completed.MessageID
	if firstAssistant == "" {
		t.Fatal("no first assistant id captured")
	}

	status, regenFrames, body := regenerateRequest(t, env.h, chatID)
	if status != 200 {
		t.Fatalf("regenerate = %d (%s)", status, body)
	}
	if regenFrames[0].name != "generation.started" || regenFrames[len(regenFrames)-1].name != "generation.completed" {
		t.Fatalf("regenerate frames = %+v", regenFrames)
	}
	rows := env.messageRows(t, chatID)
	if len(rows) != 2 {
		t.Fatalf("messages after regenerate = %d (%v), want 2", len(rows), rows)
	}
	if rows[0][0] != "user" || rows[0][1] != "hello" {
		t.Errorf("user row changed: %v", rows[0])
	}
	if rows[1][0] != "assistant" || rows[1][1] == "" {
		t.Errorf("new assistant row = %v", rows[1])
	}
	// Exactly ONE assistant row remains: the old one was deleted.
	var assistants int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_id = ? AND role = 'assistant'`,
		chatID).Scan(&assistants); err != nil {
		t.Fatal(err)
	}
	if assistants != 1 {
		t.Errorf("assistant rows after regenerate = %d, want 1 (old row deleted)", assistants)
	}
	// The new assistant id differs from the deleted one.
	var newAssistantID string
	if err := env.db.QueryRow(`SELECT id FROM messages WHERE chat_id = ? AND role = 'assistant'`,
		chatID).Scan(&newAssistantID); err != nil {
		t.Fatal(err)
	}
	if newAssistantID == firstAssistant {
		t.Errorf("assistant row was not replaced: id %q still present", firstAssistant)
	}
}

// regenerateRequest issues POST /chats/{id}/regenerate.
func regenerateRequest(t *testing.T, h http.Handler, chatID string) (int, []sseFrame, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chats/"+chatID+"/regenerate", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, parseSSE(t, rec.Body.String()), rec.Body.String()
}

func TestGenerationRegenerateValidation(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	// Empty chat → 400.
	status, body := do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/regenerate", testToken, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("regenerate empty chat = %d (%v), want 400", status, body)
	}
	if code, _ := errCode(t, body); code != "INVALID_REQUEST" {
		t.Errorf("code = %v", body)
	}

	// User message last (stream failed before the answer) → 400.
	env.mock.InjectStreamFailures(1)
	if s, _, _ := runGenerationToCompletion(t, env.h, chatID, "hello", false); s != 200 {
		t.Fatal("generate with injected failure failed")
	}
	status, body = do(t, env.h, http.MethodPost, "/api/v1/chats/"+chatID+"/regenerate", testToken, nil)
	if status != http.StatusBadRequest {
		t.Errorf("regenerate with user last = %d (%v), want 400", status, body)
	}
	if code, _ := errCode(t, body); code != "INVALID_REQUEST" {
		t.Errorf("code = %v", body)
	}

	// Unknown chat → 404.
	status, _ = do(t, env.h, http.MethodPost, "/api/v1/chats/UNKNOWN01/regenerate", testToken, nil)
	if status != http.StatusNotFound {
		t.Errorf("regenerate unknown chat = %d, want 404", status)
	}
}

func TestGenerationDisconnectCancelsAndPersistsPartial(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")

	entered := make(chan struct{})
	var once sync.Once
	env.mock.SetStreamFunc(func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		onDelta(llm.StreamDelta{Content: "部分"})
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	})
	defer env.mock.SetStreamFunc(nil)

	srv := httptest.NewServer(env.h)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		srv.URL+"/api/v1/chats/"+chatID+"/generations", strings.NewReader(`{"content":"长问题","thinking":false}`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generation: %v", err)
	}
	// Read the started event (2 lines + blank separator), then drop the
	// connection. Small line reads — never ReadFull on the whole frame.
	reader := bufio.NewReader(resp.Body)
	for i := 0; i < 3; i++ {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read started frame: %v", err)
		}
	}
	<-entered
	cancel() // client disconnect
	_ = resp.Body.Close()

	// The server must settle the generation: partial assistant row persisted.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := env.messageRows(t, chatID)
		if len(rows) == 2 && rows[1][0] == "assistant" && rows[1][1] == "部分" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("partial content not persisted after disconnect: %v", rows)
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Registry freed: a normal generation works again.
	env.mock.SetStreamFunc(nil)
	status, frames, _ := runGenerationToCompletion(t, env.h, chatID, "再来一条", false)
	if status != 200 || frames[len(frames)-1].name != "generation.completed" {
		t.Fatalf("post-disconnect generation = %d %+v", status, frames)
	}
}

func TestGenerationThinkingFlagReachesProviderRequest(t *testing.T) {
	env := newChatTestEnv(t)
	chatID := createChat(t, env.h, "")
	var got llm.StreamRequest
	var mu sync.Mutex
	env.mock.SetStreamFunc(func(ctx context.Context, req llm.StreamRequest, onDelta func(llm.StreamDelta)) error {
		mu.Lock()
		got = req
		mu.Unlock()
		onDelta(llm.StreamDelta{Content: "ok"})
		return nil
	})
	defer env.mock.SetStreamFunc(nil)

	if s, _, _ := runGenerationToCompletion(t, env.h, chatID, "hello", true); s != 200 {
		t.Fatal("generate failed")
	}
	mu.Lock()
	defer mu.Unlock()
	if !got.Thinking {
		t.Errorf("StreamRequest.Thinking = false, want true")
	}
	if got.Model != "deepseek-flash" {
		t.Errorf("StreamRequest.Model = %q", got.Model)
	}
	// Frozen compose: [system, ..., user] — the user row is the LAST message.
	if len(got.Messages) < 2 || got.Messages[len(got.Messages)-1].Role != "user" ||
		got.Messages[len(got.Messages)-1].Content != "hello" {
		t.Errorf("last provider message = %+v", got.Messages)
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != chat.DefaultSystemPrompt {
		t.Errorf("system message = %+v", got.Messages[0])
	}
}
