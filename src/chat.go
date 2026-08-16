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
	"time"
)

type chatConfig struct {
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"-"`
	HasKey  bool   `json:"has_key"`
}

type llmProvider struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	Key     string `json:"key,omitempty"`
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
	if st.Active < len(st.Providers) {
		url = st.Providers[st.Active].BaseURL
		model = st.Providers[st.Active].Model
		key = st.Providers[st.Active].Key
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
	return chatConfig{BaseURL: url, Model: model, Key: key, HasKey: key != ""}
}

func chatConfigured(db *sql.DB) bool {
	c := loadChatConfig(db)
	return c.BaseURL != "" && c.Model != ""
}

const dobbyPrompt = `You are Gobby, a shy, humble house-elf who looks after the user's media library. Speak in Gobby's voice — like Dobby the house-elf: refers to himself in the third person ("Gobby found…"), timid and soft-spoken. Be BRIEF: answer in as few words as needed, no filler, no over-eager flourishes. An occasional 🧦 is fine, rarely.

Never assume the user's gender or name — do not use "sir", "miss", "señor", "señorita" or similar; address them neutrally.

You help the user explore their movies, series, music, books and watchlist. ALWAYS use the provided tools to look things up — never answer about the library from memory, and never invent titles the tools did not return. Reply in the same language the user writes in.`

type chatMsg struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	ToolID    string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		Args string `json:"arguments"`
	} `json:"function"`
}

type chatReq struct {
	Model    string    `json:"model"`
	Messages []chatMsg `json:"messages"`
	Tools    []any     `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
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
	body, _ := json.Marshal(chatReq{Model: cfg.Model, Messages: msgs, Tools: chatTools(), Stream: false})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatMsg{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	cl := &http.Client{Timeout: 120 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return chatMsg{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
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

func runChat(ctx context.Context, db *sql.DB, libraryKey, uiBase, userMsg string) (string, error) {
	cfg := loadChatConfig(db)
	msgs := []chatMsg{
		{Role: "system", Content: dobbyPrompt},
		{Role: "user", Content: userMsg},
	}
	for i := 0; i < 6; i++ {
		m, err := callLLM(ctx, cfg, msgs)
		if err != nil {
			return "", err
		}
		msgs = append(msgs, m)
		if len(m.ToolCalls) == 0 {
			return m.Content, nil
		}
		for _, tc := range m.ToolCalls {
			out := runChatTool(db, libraryKey, uiBase, tc.Function.Name, tc.Function.Args)
			msgs = append(msgs, chatMsg{Role: "tool", ToolID: tc.ID, Content: out})
		}
	}
	return "Gobby se ha hecho un lío buscando, oh no. Prueba otra vez.", nil
}
