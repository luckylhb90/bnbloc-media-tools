package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// generate_video (docs/prd/2026-10-19-video-generation.md §2.3): the
// gateway submits the video and, polled until it is done, publishes it as
// a room file. A video takes from half a minute to a few minutes, longer
// than an agent's tool call may last (a CLI can end one at 60 seconds), so
// one call waits up to videoWait and then answers the video's id to
// continue with; no clock ends the video itself.

// videoWait is how long one call waits for its video; videoPollEvery how
// often it asks (variables: tests shorten them).
var (
	videoWait      = 50 * time.Second
	videoPollEvery = 5 * time.Second
)

var toolVideo = map[string]any{
	"name":        "generate_video",
	"description": "Make a short video clip (a few seconds) from a description; the result is a room file the person who asked you sees. It takes from half a minute to a few minutes: when a call answers rendering, call again with continue (and the same fileName) to keep waiting, which spends nothing again. Agree first what the clip is for; a video spends their organization's AI budget, far more than an image.",
	"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":   map[string]any{"type": "string", "description": "What to film, concretely: subject, action, setting, camera, style."},
			"seconds":  map[string]any{"type": "integer", "description": "The clip's length (the model's own lengths, e.g. 4, 6 or 8). Default the model's shortest."},
			"aspect":   map[string]any{"type": "string", "enum": []string{"16:9", "9:16"}, "description": "Default 16:9 (landscape); 9:16 is portrait."},
			"fileName": map[string]any{"type": "string", "description": "The file's name, e.g. teaser.mp4. Default video.mp4."},
			"continue": map[string]any{"type": "string", "description": "The id a rendering answer gave: keep waiting for that video (prompt not needed)."},
		},
	},
}

// videoReply is the gateway's answer to both video routes.
type videoReply struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Model      string `json:"model"`
	Error      string `json:"error"`
	ArtifactID string `json:"artifactId"`
	FileName   string `json:"fileName"`
	Bytes      int64  `json:"bytes"`
}

// callVideo runs generate_video: submit (or continue), then wait.
func callVideo(gw *relay, args json.RawMessage) (any, *rpcError) {
	var in struct {
		Prompt   string `json:"prompt"`
		Seconds  int    `json:"seconds"`
		Aspect   string `json:"aspect"`
		FileName string `json:"fileName"`
		Continue string `json:"continue"`
	}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid arguments: " + err.Error()}
		}
	}
	id := strings.TrimSpace(in.Continue)
	if id == "" {
		prompt := strings.TrimSpace(in.Prompt)
		if prompt == "" {
			return nil, &rpcError{Code: -32602, Message: "prompt is required (or continue with a rendering video's id)"}
		}
		body := map[string]any{"prompt": prompt}
		if in.Seconds > 0 {
			body["seconds"] = in.Seconds
		}
		if in.Aspect != "" {
			body["aspect"] = in.Aspect
		}
		raw, _ := json.Marshal(body)
		var started videoReply
		if err := gw.post("/v1/media/video", nil, "application/json", raw, &started); err != nil {
			return nil, err
		}
		if started.ID == "" {
			return nil, &rpcError{Code: -32000, Message: "the gateway started no video"}
		}
		id = started.ID
	}
	q := url.Values{"publish": {"true"}}
	if name := strings.TrimSpace(in.FileName); name != "" {
		q.Set("fileName", name)
	}
	deadline := time.Now().Add(videoWait)
	for {
		var st videoReply
		if err := gw.get("/v1/media/video/"+url.PathEscape(id), q, &st); err != nil {
			return nil, err
		}
		switch st.Status {
		case "done":
			row := map[string]any{"artifactId": st.ArtifactID, "fileName": st.FileName}
			if st.Bytes > 0 {
				row["bytes"] = st.Bytes
			}
			return textResult(map[string]any{"video": row, "note": "The video is a room file; tell the person where it is and what it shows."}), nil
		case "failed":
			msg := "the provider could not make the video"
			if st.Error != "" {
				msg += ": " + st.Error
			}
			return nil, &rpcError{Code: -32000, Message: msg}
		}
		if !time.Now().Add(videoPollEvery).Before(deadline) {
			return textResult(map[string]any{"status": "rendering", "continue": id, "fileName": q.Get("fileName"),
				"note": "Still being made. Call generate_video again with continue set to this id (and the same fileName) to keep waiting; tell the person it is on its way if they are waiting."}), nil
		}
		time.Sleep(videoPollEvery)
	}
}

// textResult is a tool result of one JSON text block.
func textResult(v any) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": mustJSON(v)}}}
}
