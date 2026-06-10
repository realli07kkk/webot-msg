package console

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestRunReturnsExitReasonCommandForExitCommands(t *testing.T) {
	for _, command := range []string{"/exit", "/quit"} {
		t.Run(command, func(t *testing.T) {
			controller := &fakeController{defaultBotID: "bot-1"}

			got := runWithInput(t, command+"\n", controller)

			if got != ExitReasonCommand {
				t.Fatalf("Run() = %v, want %v", got, ExitReasonCommand)
			}
			if controller.sendTextCalled {
				t.Fatalf("SendText was called for %s", command)
			}
		})
	}
}

func TestRunReturnsInputClosedWhenStdinCloses(t *testing.T) {
	got := runWithInput(t, "", &fakeController{})

	if got != ExitReasonInputClosed {
		t.Fatalf("Run() = %v, want %v", got, ExitReasonInputClosed)
	}
}

func TestRunKeepsActiveBotSessionLocal(t *testing.T) {
	controller := &fakeController{
		selectBotIDs: map[int]string{
			1: "bot-1",
			2: "bot-2",
		},
	}

	first := runWithInput(t, "/bot 1\nhello\n/exit\n", controller)
	second := runWithInput(t, "/bot 2\nhello\n/exit\n", controller)

	if first != ExitReasonCommand || second != ExitReasonCommand {
		t.Fatalf("Run() = %v and %v, want command exits", first, second)
	}
	if got := controller.sentBotIDs; len(got) != 2 || got[0] != "bot-1" || got[1] != "bot-2" {
		t.Fatalf("sentBotIDs = %#v, want [bot-1 bot-2]", got)
	}
}

func runWithInput(t *testing.T, input string, controller Controller) ExitReason {
	t.Helper()

	return RunWithIO(controller, bytes.NewBufferString(input), io.Discard)
}

type fakeController struct {
	defaultBotID   string
	selectBotIDs   map[int]string
	sentBotIDs     []string
	sendTextCalled bool
}

func (f *fakeController) DefaultBotID() string {
	return f.defaultBotID
}

func (f *fakeController) Login(_ io.Writer) (string, error) {
	return "bot-1", nil
}

func (f *fakeController) PrintBots(_ string, _ io.Writer) {}

func (f *fakeController) SelectBot(idx int, _ io.Writer) (string, bool) {
	botID, ok := f.selectBotIDs[idx]
	return botID, ok
}

func (f *fakeController) DeleteBot(_ int, _ io.Writer) (string, bool) {
	return "", false
}

func (f *fakeController) SendText(botID string, _ string) error {
	f.sendTextCalled = true
	f.sentBotIDs = append(f.sentBotIDs, botID)
	return errors.New("unexpected send")
}
