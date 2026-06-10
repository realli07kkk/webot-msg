package console

import (
	"errors"
	"os"
	"testing"
)

func TestRunReturnsExitReasonCommandForExitCommands(t *testing.T) {
	for _, command := range []string{"/exit", "/quit"} {
		t.Run(command, func(t *testing.T) {
			controller := &fakeController{activeBotID: "bot-1"}

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

func runWithInput(t *testing.T, input string, controller Controller) ExitReason {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	defer reader.Close()

	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("write stdin pipe: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin pipe writer: %v", err)
	}

	oldStdin := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = oldStdin
	}()

	return Run(controller)
}

type fakeController struct {
	activeBotID    string
	sendTextCalled bool
}

func (f *fakeController) ActiveBotID() string {
	return f.activeBotID
}

func (f *fakeController) Login() error {
	return nil
}

func (f *fakeController) PrintBots() {}

func (f *fakeController) SelectBot(_ int) {}

func (f *fakeController) DeleteBot(_ int) {}

func (f *fakeController) SendText(_ string) error {
	f.sendTextCalled = true
	return errors.New("unexpected send")
}
