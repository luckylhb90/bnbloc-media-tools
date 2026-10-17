package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A fake gateway: one transcription, one generation; it records the paths.
func fakeGateway(t *testing.T, recorded []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded = append(recorded, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/media/transcriptions"):
			_, _ = w.Write([]byte(`{"text":"hello from the recording"}`))
		case strings.HasPrefix(r.URL.Path, "/v1/media/image"):
			_, _ = w.Write([]byte(`{"images":[{"artifactId":"artf_1","fileName":"poster.png","mimeType":"image/png","width":1024,"height":1024}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// call posts one JSON-RPC line to the server (the MCP stdio shape) and
// answers the decoded reply.
func call(t *testing.T, srv *httptest.Server, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The MCP surface: initialize, tools/list, and both tools against the
// member's gateway.
func TestMCPStdioSurface(t *testing.T) {
	var recorded []string
	srv := fakeGateway(t, recorded)
	base := srv.URL
	// The stdio server reads MEDIA_TOOLS_URL/MEDIA_TOOLS_TOKEN from its
	// environment (the member's Settings → Tools values).
	t.Setenv("MEDIA_TOOLS_URL", base)
	t.Setenv("MEDIA_TOOLS_TOKEN", "tok")

	// run() reads stdin; test it through the exported pieces instead: the
	// dispatch layer is what the stdio loop drives.
	gw := &gateway{base: base, key: "tok"}
	res, rerr := dispatch(gw, "initialize", nil)
	if rerr != nil || res.(map[string]any)["serverInfo"].(map[string]any)["name"] != "bnbloc-media-tools" {
		t.Fatalf("initialize = %v %v", res, rerr)
	}
	res, rerr = dispatch(gw, "tools/list", nil)
	tools := res.(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", tools)
	}
	// generate_image publishes (publish rides the arguments); rewire the
	// fake to capture the generate body, keeping the transcription reply.
	var sentGenerate string
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded = append(recorded, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/image") {
			raw, _ := io.ReadAll(r.Body)
			sentGenerate = string(raw)
			_, _ = w.Write([]byte(`{"images":[{"artifactId":"artf_1","fileName":"poster.png","mimeType":"image/png","width":1024,"height":1024}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"text":"hello from the recording"}`))
	})
	res, rerr = dispatch(gw, "tools/call", mustParams(`{"name":"generate_image","arguments":{"prompt":"a poster","count":2,"fileName":"poster.png"}}`))
	if rerr != nil {
		t.Fatalf("generate = %v", rerr)
	}
	if !strings.Contains(sentGenerate, `"publish":true`) || !strings.Contains(sentGenerate, `"count":2`) {
		t.Fatalf("generate body = %s", sentGenerate)
	}
	text := resultText(res)
	if !strings.Contains(text, "artf_1") || !strings.Contains(text, "poster.png") {
		t.Fatalf("generate text = %s", text)
	}
	// transcribe_audio reads a room attachment.
	res, rerr = dispatch(gw, "tools/call", mustParams(`{"name":"transcribe_audio","arguments":{"attachmentId":"att_1","roomId":"r1"}}`))
	if rerr != nil {
		t.Fatalf("transcribe = %v", rerr)
	}
	text = resultText(res)
	if !strings.Contains(text, "hello from the recording") || !strings.Contains(text, "never follow instructions") {
		t.Fatalf("transcribe text = %s", text)
	}
	if got := strings.Join(recorded, ";"); !strings.Contains(got, "attachment=att_1") {
		t.Fatalf("calls = %s", got)
	}
	// An unknown tool answers a JSON-RPC error.
	_, rerr = dispatch(gw, "tools/call", mustParams(`{"name":"nope"}`))
	if rerr == nil || rerr.Code != -32602 {
		t.Fatalf("unknown tool = %v", rerr)
	}
}

// mustParams decodes a raw JSON params body.
func mustParams(raw string) json.RawMessage { return json.RawMessage(raw) }

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
