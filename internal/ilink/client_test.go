package ilink

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/realli07kkk/webot-msg/internal/config"
)

func TestClientUsesInjectedSharedTransportWithPerCallTimeouts(t *testing.T) {
	transport := &recordingTransport{}
	client := NewClientWithTransport("https://ilink.example.com/", transport)

	user := config.UserConfig{
		BotToken:      "bot-token",
		IlinkUserID:   "user-1",
		ContextToken:  "ctx-1",
		GetUpdatesBuf: "buf-1",
	}
	if err := client.SendMessage(user, "user-1", "hello", "ctx-1"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if _, err := client.GetUpdates(user, 1500*time.Millisecond); err != nil {
		t.Fatalf("GetUpdates() error = %v", err)
	}

	if client.transport != transport {
		t.Fatal("client transport is not the injected transport")
	}
	if got := strings.Join(transport.paths, ","); got != "/ilink/bot/sendmessage,/ilink/bot/getupdates" {
		t.Fatalf("paths = %q", got)
	}
	assertTimeoutNear(t, transport.timeouts[0], 10*time.Second)
	assertTimeoutNear(t, transport.timeouts[1], 1500*time.Millisecond)
}

type recordingTransport struct {
	paths    []string
	timeouts []time.Duration
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.paths = append(r.paths, req.URL.Path)
	deadline, ok := req.Context().Deadline()
	if !ok {
		r.timeouts = append(r.timeouts, 0)
	} else {
		r.timeouts = append(r.timeouts, time.Until(deadline))
	}

	body := `{"ret":0,"errcode":0}`
	if req.URL.Path == "/ilink/bot/getupdates" {
		body = `{"ret":0,"errcode":0,"get_updates_buf":"next","longpolling_timeout_ms":0,"msgs":[]}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func assertTimeoutNear(t *testing.T, got time.Duration, want time.Duration) {
	t.Helper()
	if got <= 0 {
		t.Fatalf("timeout = %s, want near %s", got, want)
	}
	if got < want-time.Second || got > want+time.Second {
		t.Fatalf("timeout = %s, want near %s", got, want)
	}
}
