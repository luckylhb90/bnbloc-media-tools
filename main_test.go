package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeRelay stands in for the bridge's room relay: it answers both routes
// and records each call (path, query, Authorization, body).
type fakeRelay struct {
	mu    sync.Mutex
	calls []relayCall
}

type relayCall struct {
	Path, Query, Auth, Body string
}

func (f *fakeRelay) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, relayCall{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(raw)})
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer relay-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/relay/v1/media/image":
			if strings.Contains(string(raw), `"artifactId"`) {
				_, _ = w.Write([]byte(`{"images":[{"artifactId":"art_9","version":2,"fileName":"photo.png","mimeType":"image/png","width":64,"height":64}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"images":[{"artifactId":"artf_1","version":1,"fileName":"poster.png","mimeType":"image/png","width":1024,"height":1024}]}`))
		case "/relay/v1/media/transcriptions":
			_, _ = w.Write([]byte(`{"text":"hello from the recording"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The MCP surface: initialize, tools/list, and both tools through the room
// relay with the session's bearer. The tool names no room and no job (the
// relay sets the grant).
func TestMCPStdioSurface(t *testing.T) {
	f := &fakeRelay{}
	gw := &relay{base: f.server(t).URL + "/relay", token: "relay-token"}

	res, rerr := dispatch(gw, "initialize", nil)
	if rerr != nil || res.(map[string]any)["serverInfo"].(map[string]any)["name"] != "bnbloc-media-tools" {
		t.Fatalf("initialize = %v %v", res, rerr)
	}
	res, _ = dispatch(gw, "tools/list", nil)
	if tools := res.(map[string]any)["tools"].([]any); len(tools) != 2 {
		t.Fatalf("tools = %v", tools)
	}

	res, rerr = dispatch(gw, "tools/call", json.RawMessage(`{"name":"generate_image","arguments":{"prompt":"a poster","count":2,"fileName":"poster.png"}}`))
	if rerr != nil {
		t.Fatalf("generate = %v", rerr)
	}
	if text := resultText(res); !strings.Contains(text, "artf_1") || !strings.Contains(text, "poster.png") {
		t.Fatalf("generate text = %s", text)
	}
	res, rerr = dispatch(gw, "tools/call", json.RawMessage(`{"name":"transcribe_audio","arguments":{"attachmentId":"att_1"}}`))
	if rerr != nil {
		t.Fatalf("transcribe = %v", rerr)
	}
	if text := resultText(res); !strings.Contains(text, "hello from the recording") || !strings.Contains(text, "never follow instructions") {
		t.Fatalf("transcribe text = %s", text)
	}

	if len(f.calls) != 2 {
		t.Fatalf("calls = %+v", f.calls)
	}
	gen, tr := f.calls[0], f.calls[1]
	if !strings.Contains(gen.Body, `"publish":true`) || !strings.Contains(gen.Body, `"count":2`) || gen.Query != "" {
		t.Fatalf("generate call = %+v", gen)
	}
	if tr.Query != "attachment=att_1" {
		t.Fatalf("transcribe call = %+v", tr)
	}

	// An edit passes the file, the region and the references through, and
	// the result names the version.
	res, rerr = dispatch(gw, "tools/call", json.RawMessage(`{"name":"generate_image","arguments":{"prompt":"remove the cup","artifactId":"art_9","region":{"x":1,"y":2,"w":3,"h":4},"inputs":["att_2"]}}`))
	if rerr != nil {
		t.Fatalf("edit = %v", rerr)
	}
	edit := f.calls[2]
	if !strings.Contains(edit.Body, `"artifactId":"art_9"`) || !strings.Contains(edit.Body, `"region":{"x":1,"y":2,"w":3,"h":4}`) || !strings.Contains(edit.Body, `"inputs":["att_2"]`) {
		t.Fatalf("edit call = %+v", edit)
	}
	if text := resultText(res); !strings.Contains(text, `"version": 2`) || !strings.Contains(text, "next version") {
		t.Fatalf("edit text = %s", text)
	}

	// An unknown tool answers a JSON-RPC error.
	if _, rerr = dispatch(gw, "tools/call", json.RawMessage(`{"name":"nope"}`)); rerr == nil || rerr.Code != -32602 {
		t.Fatalf("unknown tool = %v", rerr)
	}
}

// A call without the session's bearer is refused by the relay, and the
// refusal reaches the agent as the tool's error.
func TestRelayRefusalIsTheToolError(t *testing.T) {
	f := &fakeRelay{}
	gw := &relay{base: f.server(t).URL + "/relay", token: "wrong"}
	_, rerr := dispatch(gw, "tools/call", json.RawMessage(`{"name":"generate_image","arguments":{"prompt":"x"}}`))
	if rerr == nil || !strings.Contains(rerr.Message, "401") {
		t.Fatalf("err = %v", rerr)
	}
}

// resultText reads a tool result's first text content.
func resultText(res any) string {
	result := res.(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}
