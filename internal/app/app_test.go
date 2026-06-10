package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realli07kkk/webot-msg/internal/config"
	"github.com/realli07kkk/webot-msg/internal/ilink"
)

func TestPersistUpdateStateStoresReplyTargetAndContextTogether(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "auth.json"))
	if err := store.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}
	if err := store.AddBot(config.UserConfig{
		BotID:        "bot-1",
		IlinkUserID:  "old-user",
		ContextToken: "old-context",
	}); err != nil {
		t.Fatalf("AddBot() error = %v", err)
	}

	a := &App{store: store}
	a.persistUpdateState("bot-1", &ilink.UpdatesResponse{
		GetUpdatesBuf: "buf-1",
		Msgs: []ilink.WeixinMessage{
			{
				FromUserID:   "new-user",
				ContextToken: "new-context",
			},
		},
	})

	user, ok := store.GetBot("bot-1")
	if !ok {
		t.Fatal("GetBot() ok = false, want true")
	}
	if user.IlinkUserID != "new-user" {
		t.Fatalf("IlinkUserID = %q, want %q", user.IlinkUserID, "new-user")
	}
	if user.ContextToken != "new-context" {
		t.Fatalf("ContextToken = %q, want %q", user.ContextToken, "new-context")
	}
	if user.GetUpdatesBuf != "buf-1" {
		t.Fatalf("GetUpdatesBuf = %q, want %q", user.GetUpdatesBuf, "buf-1")
	}
}

func TestPrintMessagesBroadcastsToRegisteredConsoleOutput(t *testing.T) {
	a := &App{}
	var out bytes.Buffer
	unregister := a.AddConsoleOutput(&out)

	a.printMessages("bot-1", []ilink.WeixinMessage{
		{
			FromUserID: "user-1",
			ItemList: []ilink.MessageItem{
				{
					Type: 1,
					TextItem: struct {
						Text string `json:"text"`
					}{Text: "hello"},
				},
			},
		},
	})

	got := out.String()
	if !strings.Contains(got, "[Bot: bot-1 | Message from user-1]: hello") {
		t.Fatalf("broadcast output = %q, want message text", got)
	}

	unregister()
	out.Reset()
	a.printMessages("bot-1", []ilink.WeixinMessage{
		{
			FromUserID: "user-1",
			ItemList: []ilink.MessageItem{
				{
					Type: 1,
					TextItem: struct {
						Text string `json:"text"`
					}{Text: "after-unregister"},
				},
			},
		},
	})
	if got := out.String(); got != "" {
		t.Fatalf("broadcast after unregister = %q, want empty", got)
	}
}
