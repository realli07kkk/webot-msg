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

func TestRunReturnsInterruptForReaderInterrupt(t *testing.T) {
	got := RunWithLineReader(&fakeController{}, interruptingLineReader{}, io.Discard)

	if got != ExitReasonInterrupt {
		t.Fatalf("Run() = %v, want %v", got, ExitReasonInterrupt)
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

func TestRunSwitchesActiveBotAfterLogin(t *testing.T) {
	controller := &fakeController{
		defaultBotID: "old-bot",
		loginBotID:   "new-bot",
	}

	got := runWithInput(t, "/login\nhello\n/exit\n", controller)

	if got != ExitReasonCommand {
		t.Fatalf("Run() = %v, want %v", got, ExitReasonCommand)
	}
	if got := controller.sentBotIDs; len(got) != 1 || got[0] != "new-bot" {
		t.Fatalf("sentBotIDs = %#v, want [new-bot]", got)
	}
}

func TestRunHandlesProtectionCommands(t *testing.T) {
	controller := &fakeController{defaultBotID: "bot-1"}

	got := runWithInput(t, "/protection enable\n/protection status\n/protection disable\n/exit\n", controller)

	if got != ExitReasonCommand {
		t.Fatalf("Run() = %v, want %v", got, ExitReasonCommand)
	}
	if controller.enableProtectionCalls != 1 {
		t.Fatalf("enableProtectionCalls = %d, want 1", controller.enableProtectionCalls)
	}
	if controller.disableProtectionCalls != 1 {
		t.Fatalf("disableProtectionCalls = %d, want 1", controller.disableProtectionCalls)
	}
	if got := controller.statusBotIDs; len(got) != 1 || got[0] != "bot-1" {
		t.Fatalf("statusBotIDs = %#v, want [bot-1]", got)
	}
	if controller.sendTextCalled {
		t.Fatal("SendText was called for protection commands")
	}
}

func TestRunHandlesAuditCommands(t *testing.T) {
	controller := &fakeController{defaultBotID: "bot-1"}

	got := runWithInput(t, "/audit enable\n/audit status\n/audit disable\n/exit\n", controller)

	if got != ExitReasonCommand {
		t.Fatalf("Run() = %v, want %v", got, ExitReasonCommand)
	}
	if controller.enableAuditCalls != 1 {
		t.Fatalf("enableAuditCalls = %d, want 1", controller.enableAuditCalls)
	}
	if controller.disableAuditCalls != 1 {
		t.Fatalf("disableAuditCalls = %d, want 1", controller.disableAuditCalls)
	}
	if controller.auditStatusCalls != 1 {
		t.Fatalf("auditStatusCalls = %d, want 1", controller.auditStatusCalls)
	}
	if controller.sendTextCalled {
		t.Fatal("SendText was called for audit commands")
	}
}

func TestRunTreatsProtectionPrefixWithoutSeparatorAsText(t *testing.T) {
	controller := &fakeController{defaultBotID: "bot-1"}

	got := runWithInput(t, "/protectionfoo\n/exit\n", controller)

	if got != ExitReasonCommand {
		t.Fatalf("Run() = %v, want %v", got, ExitReasonCommand)
	}
	if controller.enableProtectionCalls != 0 || controller.disableProtectionCalls != 0 || len(controller.statusBotIDs) != 0 {
		t.Fatal("/protectionfoo was handled as a protection command")
	}
	if !controller.sendTextCalled {
		t.Fatal("SendText was not called for /protectionfoo")
	}
}

func runWithInput(t *testing.T, input string, controller Controller) ExitReason {
	t.Helper()

	return RunWithIO(controller, bytes.NewBufferString(input), io.Discard)
}

type fakeController struct {
	defaultBotID   string
	loginBotID     string
	selectBotIDs   map[int]string
	sentBotIDs     []string
	statusBotIDs   []string
	sendTextCalled bool

	enableProtectionCalls  int
	disableProtectionCalls int
	enableAuditCalls       int
	disableAuditCalls      int
	auditStatusCalls       int
}

type interruptingLineReader struct{}

func (interruptingLineReader) ReadLine(string) (string, error) {
	return "", ErrInterrupted
}

func (interruptingLineReader) Close() error {
	return nil
}

func (f *fakeController) DefaultBotID() string {
	return f.defaultBotID
}

func (f *fakeController) Login(_ io.Writer) (string, error) {
	if f.loginBotID != "" {
		return f.loginBotID, nil
	}
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

func (f *fakeController) EnableProtection(_ io.Writer) error {
	f.enableProtectionCalls++
	return nil
}

func (f *fakeController) DisableProtection(_ io.Writer) error {
	f.disableProtectionCalls++
	return nil
}

func (f *fakeController) PrintProtectionStatus(botID string, _ io.Writer) {
	f.statusBotIDs = append(f.statusBotIDs, botID)
}

func (f *fakeController) EnableAudit(_ io.Writer) error {
	f.enableAuditCalls++
	return nil
}

func (f *fakeController) DisableAudit(_ io.Writer) error {
	f.disableAuditCalls++
	return nil
}

func (f *fakeController) PrintAuditStatus(_ io.Writer) {
	f.auditStatusCalls++
}

func (f *fakeController) SendText(botID string, _ string) error {
	f.sendTextCalled = true
	f.sentBotIDs = append(f.sentBotIDs, botID)
	return errors.New("unexpected send")
}
