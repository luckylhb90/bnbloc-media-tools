package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// videoRelay answers the video routes: a submit, then status polls that
// stay rendering for polls and then end as end ("done" or "failed").
type videoRelay struct {
	mu      sync.Mutex
	polls   int
	renders int
	end     string
	submits []string
	queries []string
}

func (f *videoRelay) server(t *testing.T) *relay {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/relay/v1/media/video":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			raw, _ := json.Marshal(body)
			f.submits = append(f.submits, string(raw))
			_, _ = w.Write([]byte(`{"id":"video_a+b=","status":"rendering","model":"veo"}`))
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/relay/v1/media/video/video_a+b=":
			f.polls++
			f.queries = append(f.queries, r.URL.RawQuery)
			switch {
			case f.polls <= f.renders:
				_, _ = w.Write([]byte(`{"id":"video_a+b=","status":"rendering"}`))
			case f.end == "failed":
				_, _ = w.Write([]byte(`{"id":"video_a+b=","status":"failed","error":"the prompt was refused"}`))
			default:
				_, _ = w.Write([]byte(`{"id":"video_a+b=","status":"done","artifactId":"art_v","fileName":"boat.mp4","bytes":1454775}`))
			}
		default:
			http.Error(w, r.Method+" "+r.URL.EscapedPath(), http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &relay{base: srv.URL + "/relay", token: "relay-token"}
}

func fastVideo(t *testing.T, wait time.Duration) {
	t.Helper()
	w, e := videoWait, videoPollEvery
	videoWait, videoPollEvery = wait, 10*time.Millisecond
	t.Cleanup(func() { videoWait, videoPollEvery = w, e })
}

// A video is submitted once and polled until done: the room file, with
// the name asked for published.
func TestVideoWaitsUntilDone(t *testing.T) {
	fastVideo(t, time.Second)
	f := &videoRelay{renders: 3}
	res, rerr := callVideo(f.server(t), json.RawMessage(`{"prompt":"a paper boat","seconds":4,"aspect":"16:9","fileName":"boat.mp4"}`))
	if rerr != nil {
		t.Fatal(rerr.Message)
	}
	if text := resultText(res); !strings.Contains(text, "art_v") || !strings.Contains(text, "boat.mp4") {
		t.Fatalf("result = %s", text)
	}
	if len(f.submits) != 1 || f.submits[0] != `{"aspect":"16:9","prompt":"a paper boat","seconds":4}` || f.polls != 4 {
		t.Fatalf("submits %v, polls %d", f.submits, f.polls)
	}
	if f.queries[0] != "fileName=boat.mp4&publish=true" {
		t.Fatalf("poll query = %s", f.queries[0])
	}
}

// A video not done within one call's wait answers its id; continue waits
// on it without submitting again.
func TestVideoAnswersRenderingThenContinues(t *testing.T) {
	fastVideo(t, 50*time.Millisecond)
	f := &videoRelay{renders: 1000}
	gw := f.server(t)
	res, rerr := callVideo(gw, json.RawMessage(`{"prompt":"a paper boat"}`))
	if rerr != nil {
		t.Fatal(rerr.Message)
	}
	text := resultText(res)
	if !strings.Contains(text, `"status": "rendering"`) || !strings.Contains(text, `"continue": "video_a+b="`) {
		t.Fatalf("result = %s", text)
	}
	f.mu.Lock()
	f.renders = f.polls
	f.mu.Unlock()
	res, rerr = callVideo(gw, json.RawMessage(`{"continue":"video_a+b="}`))
	if rerr != nil || !strings.Contains(resultText(res), "art_v") || len(f.submits) != 1 {
		t.Fatalf("continue = %v %v (submits %v)", res, rerr, f.submits)
	}
}

// A failed video is the tool's error, with the provider's reason; a call
// without a prompt or continue is refused before any request.
func TestVideoFailureAndArguments(t *testing.T) {
	fastVideo(t, time.Second)
	f := &videoRelay{end: "failed"}
	if _, rerr := callVideo(f.server(t), json.RawMessage(`{"prompt":"x"}`)); rerr == nil || !strings.Contains(rerr.Message, "the prompt was refused") {
		t.Fatalf("failed = %v", rerr)
	}
	g := &videoRelay{}
	if _, rerr := callVideo(g.server(t), json.RawMessage(`{}`)); rerr == nil || rerr.Code != -32602 || len(g.submits) != 0 {
		t.Fatalf("no prompt = %v", rerr)
	}
}
