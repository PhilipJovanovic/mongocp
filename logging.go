package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	logBodyLimit  = 4 << 10 // captured per body; longer bodies are truncated
	logMaxEntries = 100
)

type logEntry struct {
	Time             time.Time         `json:"time"`
	Method           string            `json:"method"`
	Path             string            `json:"path"`
	Status           int               `json:"status"`
	DurationMS       int64             `json:"durationMs"`
	RequestHeaders   map[string]string `json:"requestHeaders"`
	RequestBody      string            `json:"requestBody,omitempty"`
	ResponseBody     string            `json:"responseBody,omitempty"`
	RequestTruncated bool              `json:"requestTruncated,omitempty"`
}

type requestLog struct {
	mu      sync.Mutex
	entries []logEntry
}

func (l *requestLog) add(e logEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	if len(l.entries) > logMaxEntries {
		l.entries = l.entries[len(l.entries)-logMaxEntries:]
	}
}

// newestFirst returns a copy of the captured entries, newest first.
func (l *requestLog) newestFirst() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]logEntry, len(l.entries))
	for i, e := range l.entries {
		out[len(l.entries)-1-i] = e
	}
	return out
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.body.Len() < logBodyLimit {
		r.body.Write(b[:min(len(b), logBodyLimit-r.body.Len())])
	}
	return r.ResponseWriter.Write(b)
}

// logRequests captures every request and response, writes them to stdout,
// and keeps the last entries in memory for GET /debug/requests.
func (a *app) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/requests" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()

		var reqBody []byte
		truncated := false
		if r.Body != nil {
			reqBody, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
			if len(reqBody) > logBodyLimit {
				reqBody = reqBody[:logBodyLimit]
				truncated = true
			}
		}

		headers := make(map[string]string, len(r.Header))
		for k := range r.Header {
			if strings.EqualFold(k, "Authorization") {
				headers[k] = "Bearer ***redacted***"
				continue
			}
			headers[k] = r.Header.Get(k)
		}

		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		entry := logEntry{
			Time:             start.UTC(),
			Method:           r.Method,
			Path:             r.URL.RequestURI(),
			Status:           rec.status,
			DurationMS:       time.Since(start).Milliseconds(),
			RequestHeaders:   headers,
			RequestBody:      string(reqBody),
			ResponseBody:     rec.body.String(),
			RequestTruncated: truncated,
		}
		a.reqLog.add(entry)

		log.Printf("%s %s -> %d (%dms)\n  request:  %s\n  response: %s",
			entry.Method, entry.Path, entry.Status, entry.DurationMS,
			emptyDash(entry.RequestBody), emptyDash(entry.ResponseBody))
	})
}

// GET /debug/requests — the last captured requests, newest first.
func (a *app) handleDebugRequests(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"requests": a.reqLog.newestFirst()})
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
