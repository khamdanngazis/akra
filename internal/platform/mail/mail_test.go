package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestNew(t *testing.T) {
	if m, err := New(Config{Driver: DriverLog}, discard); err != nil {
		t.Errorf("log: error = %v", err)
	} else if _, ok := m.(*LogMailer); !ok {
		t.Errorf("log: got %T", m)
	}

	if m, err := New(Config{Driver: DriverResend, ResendAPIKey: "re_x", From: "AKRA <a@b.id>"}, discard); err != nil {
		t.Errorf("resend: error = %v", err)
	} else if _, ok := m.(*ResendMailer); !ok {
		t.Errorf("resend: got %T", m)
	}

	if _, err := New(Config{Driver: DriverResend}, discard); err == nil || !strings.Contains(err.Error(), "RESEND_API_KEY") {
		t.Errorf("resend without key: error = %v", err)
	}
	if _, err := New(Config{Driver: "smtp"}, discard); err == nil {
		t.Error("unknown driver: error = nil")
	}
}

func TestLogMailer(t *testing.T) {
	var buf bytes.Buffer
	m := &LogMailer{Logger: slog.New(slog.NewJSONHandler(&buf, nil))}
	if err := m.Send(context.Background(), Message{To: "a@b.id", Subject: "Hai", Text: "tautan"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"level":"WARN"`, `"to":"a@b.id"`, `"subject":"Hai"`, `"text":"tautan"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log does not contain %s: %s", want, buf.String())
		}
	}
}

func TestResendMailer(t *testing.T) {
	var got map[string]any
	var auth, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, contentType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	m := &ResendMailer{APIKey: "re_key", From: "AKRA <no-reply@akra.id>", Client: srv.Client(), Endpoint: srv.URL}
	if err := m.Send(context.Background(), Message{To: "x@y.id", Subject: "S", Text: "T"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if auth != "Bearer re_key" || contentType != "application/json" {
		t.Errorf("headers: Authorization = %q, Content-Type = %q", auth, contentType)
	}
	to, _ := got["to"].([]any)
	if got["from"] != "AKRA <no-reply@akra.id>" || len(to) != 1 || to[0] != "x@y.id" || got["subject"] != "S" || got["text"] != "T" {
		t.Errorf("body = %v", got)
	}
}

func TestResendMailer_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"API key is invalid"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := &ResendMailer{APIKey: "re_secret", Client: srv.Client(), Endpoint: srv.URL}
	err := m.Send(context.Background(), Message{To: "x@y.id"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "API key is invalid") {
		t.Errorf("error = %v, want status 401 with response body", err)
	}
	if err != nil && strings.Contains(err.Error(), "re_secret") {
		t.Error("error leaks the API key")
	}
}
