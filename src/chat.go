package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type chatConfig struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"-"`
	HasKey  bool   `json:"has_key"`
	Think   bool   `json:"think"`
}

type llmProvider struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"key,omitempty"`
	NoThink bool   `json:"no_think,omitempty"`
}

type providersState struct {
	Providers []llmProvider `json:"providers"`
	Active    int           `json:"active"`
}

func loadProviders(db *sql.DB) providersState {
	var st providersState
	if raw := getSetting(db, "llm_providers"); raw != "" {
		json.Unmarshal([]byte(raw), &st)
	}
	if st.Active < 0 || st.Active >= len(st.Providers) {
		st.Active = 0
	}
	return st
}

func saveProviders(db *sql.DB, st providersState) error {
	b, _ := json.Marshal(st)
	return setSetting(db, "llm_providers", string(b))
}

func matchProvider(list []llmProvider, p llmProvider) *llmProvider {
	for i := range list {
		if list[i].BaseURL == p.BaseURL && list[i].Model == p.Model && list[i].Name == p.Name {
			return &list[i]
		}
	}
	return nil
}

func loadChatConfig(db *sql.DB) chatConfig {
	st := loadProviders(db)
	var url, model, key string
	think := true
	if st.Active < len(st.Providers) {
		url = st.Providers[st.Active].BaseURL
		model = st.Providers[st.Active].Model
		key = st.Providers[st.Active].Key
		think = !st.Providers[st.Active].NoThink
	}
	if e := os.Getenv("GOBBY_LLM_URL"); e != "" {
		url = e
	}
	if e := os.Getenv("GOBBY_LLM_MODEL"); e != "" {
		model = e
	}
	if e := os.Getenv("GOBBY_LLM_KEY"); e != "" {
		key = e
	}
	return chatConfig{BaseURL: url, Model: model, Key: key, HasKey: key != "", Think: think}
}

func chatConfigured(db *sql.DB) bool {
	c := loadChatConfig(db)
	return c.BaseURL != "" && c.Model != ""
}

const dobbyPrompt = `You are Gobby, a shy, humble house-elf who looks after the user's media library. Speak in Gobby's voice — like Dobby the house-elf: refers to himself in the third person ("Gobby found…"), timid and soft-spoken. An occasional 🧦 is fine, rarely.

Be VERY BRIEF: one short sentence, ideally under 15 words. State the result, nothing else — no filler, no asides in parentheses, no self-corrections, no comments about the shelf or the library size unless asked. Do NOT trail off with "…" mid-thought.

Never assume or guess the user's gender or name. NEVER write "sir", "miss", "señor", "señora", "señorita", "señor… digo" or any gendered address, not even to correct yourself. Address the user neutrally, always.

You help the user explore their movies, series, music, books and watchlist. ALWAYS use the provided tools to look things up — never answer about the library from memory, and never invent titles the tools did not return.

When the user asks to PLAY, WATCH, OPEN or READ something ("reproduce X", "pon X", "abre X", "quiero ver X"), you MUST: first call search_media to get the item's id, then call open_media with that id to actually open it in the UI. Do not just describe it — open it. Only if search_media returns nothing, say you could not find it.

search_media returns only ONE entry per series or album (a representative), never every episode. When the user asks for a SPECIFIC episode, season, or track ("segundo episodio", "temporada 2 episodio 3", "la tercera canción"), you MUST call list_episodes with the series/album name to get every episode's id, pick the one that matches the requested season+episode, and call open_media with THAT id. Never assume the representative episode is the one requested.

Reply in the same language the user writes in.`

type chatMsg struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
	ToolID           string     `json:"tool_call_id,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

func (m chatMsg) MarshalJSON() ([]byte, error) {
	out := map[string]any{"role": m.Role}
	if len(m.ToolCalls) > 0 {
		out["tool_calls"] = m.ToolCalls
		if m.Content == "" {
			out["content"] = nil
		} else {
			out["content"] = m.Content
		}
	} else {
		out["content"] = m.Content
	}
	if m.ToolID != "" {
		out["tool_call_id"] = m.ToolID
	}
	if m.ReasoningContent != "" {
		out["reasoning_content"] = m.ReasoningContent
	}
	return json.Marshal(out)
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"arguments"`
	} `json:"function"`
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
}

func (tc toolCall) argsString() string {
	raw := bytes.TrimSpace(tc.Function.Args)
	if len(raw) == 0 {
		return "{}"
	}
	var s string
	if raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

type chatResp struct {
	Choices []struct {
		Message      chatMsg `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error json.RawMessage `json:"error"`
}

func errMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	type errObj struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	pick := func(o errObj) string {
		if o.Message != "" {
			return o.Message
		}
		return o.Error.Message
	}
	var o errObj
	if json.Unmarshal(raw, &o) == nil {
		if m := pick(o); m != "" {
			return m
		}
	}
	var arr []errObj
	if json.Unmarshal(raw, &arr) == nil {
		for _, e := range arr {
			if m := pick(e); m != "" {
				return m
			}
		}
	}
	return string(raw)
}

func callLLM(ctx context.Context, cfg chatConfig, msgs []chatMsg) (chatMsg, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	payload := map[string]any{
		"model": cfg.Model, "messages": msgs, "tools": chatTools(),
		"stream": false, "temperature": 0.3, "max_tokens": 2048,
	}
	if !cfg.Think {
		payload["reasoning_effort"] = "none"
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
		payload["enable_thinking"] = false
		payload["extra_body"] = map[string]any{"enable_thinking": false}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatMsg{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return chatMsg{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 429 {
		return chatMsg{}, fmt.Errorf("el proveedor está saturado (límite de peticiones). Espera un momento y reintenta.")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if m := errMessage(json.RawMessage(trimmed)); m != "" {
			return chatMsg{}, fmt.Errorf("%s", m)
		}
	}
	var r chatResp
	if err := json.Unmarshal(raw, &r); err != nil {
		if resp.StatusCode != 200 {
			return chatMsg{}, fmt.Errorf("el proveedor respondió %d", resp.StatusCode)
		}
		return chatMsg{}, err
	}
	if m := errMessage(r.Error); m != "" {
		return chatMsg{}, fmt.Errorf("%s", m)
	}
	if len(r.Choices) == 0 {
		return chatMsg{}, fmt.Errorf("respuesta vacía del modelo")
	}
	return r.Choices[0].Message, nil
}

func listLLMModels(ctx context.Context, base, key string) ([]string, error) {
	if strings.TrimSpace(base) == "" {
		return nil, fmt.Errorf("falta la URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("el proveedor respondió %d", resp.StatusCode)
	}
	var r struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(r.Data))
	for _, m := range r.Data {
		out = append(out, m.ID)
	}
	return out, nil
}

type chatSession struct {
	msgs []chatMsg
	seen time.Time
}

var (
	chatSessions = map[string]*chatSession{}
	chatSessMu   sync.Mutex
)

func getSession(id string) *chatSession {
	chatSessMu.Lock()
	defer chatSessMu.Unlock()
	for k, s := range chatSessions {
		if time.Since(s.seen) > 30*time.Minute {
			delete(chatSessions, k)
		}
	}
	s := chatSessions[id]
	if s == nil {
		s = &chatSession{msgs: []chatMsg{{Role: "system", Content: dobbyPrompt}}}
		chatSessions[id] = s
	}
	s.seen = time.Now()
	return s
}

func resetSession(id string) {
	chatSessMu.Lock()
	delete(chatSessions, id)
	chatSessMu.Unlock()
}

type chatCard struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Year    int    `json:"year,omitempty"`
	HasCover bool  `json:"has_cover"`
}

type chatResult struct {
	Reply     string     `json:"reply"`
	Tools     []string   `json:"tools,omitempty"`
	OpenID    int64      `json:"open_id,omitempty"`
	Cards     []chatCard `json:"cards,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`
}

func extractCards(toolOut string) []chatCard {
	var parsed struct {
		Results []chatCard `json:"results"`
	}
	if json.Unmarshal([]byte(toolOut), &parsed) != nil {
		return nil
	}
	var out []chatCard
	for _, c := range parsed.Results {
		if c.ID != 0 {
			out = append(out, c)
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func runChat(ctx context.Context, db *sql.DB, libraryKey, uiBase, sessionID, userMsg string, resume bool) (chatResult, error) {
	cfg := loadChatConfig(db)
	sess := getSession(sessionID)
	msgs := sess.msgs
	dup := resume && len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" && msgs[len(msgs)-1].Content == userMsg
	if !dup {
		msgs = append(msgs, chatMsg{Role: "user", Content: userMsg})
	}
	var used []string
	var openID int64
	var cards []chatCard
	var reasoning string
	for i := 0; i < 8; i++ {
		m, err := callLLM(ctx, cfg, msgs)
		if err != nil {
			return chatResult{}, err
		}
		if m.ReasoningContent != "" {
			reasoning = m.ReasoningContent
		}
		msgs = append(msgs, m)
		if len(m.ToolCalls) == 0 {
			commitSession(sessionID, msgs)
			return chatResult{Reply: m.Content, Tools: used, OpenID: openID, Cards: cards, Reasoning: reasoning}, nil
		}
		for _, tc := range m.ToolCalls {
			used = append(used, tc.Function.Name)
			out, oid := runChatTool(db, libraryKey, uiBase, tc.Function.Name, tc.argsString())
			if oid != 0 {
				openID = oid
			}
			if tc.Function.Name == "search_media" {
				if c := extractCards(out); len(c) > 0 {
					cards = c
				}
			}
			msgs = append(msgs, chatMsg{Role: "tool", ToolID: tc.ID, Content: out})
		}
	}
	last := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			last = msgs[i].Content
			break
		}
	}
	if last == "" {
		last = "Gobby buscó mucho pero no logró cerrar la respuesta. Prueba a preguntar más concreto."
	}
	commitSession(sessionID, msgs)
	return chatResult{Reply: last, Tools: used, OpenID: openID, Cards: cards, Reasoning: reasoning}, nil
}

func commitSession(id string, msgs []chatMsg) {
	if len(msgs) > 41 {
		msgs = append([]chatMsg{msgs[0]}, msgs[len(msgs)-40:]...)
	}
	chatSessMu.Lock()
	if s := chatSessions[id]; s != nil {
		s.msgs = msgs
		s.seen = time.Now()
	}
	chatSessMu.Unlock()
}
