package control

import (
	"reflect"
	"testing"
)

func TestOutputSplitterSplitsLinesAndPromptTail(t *testing.T) {
	var splitter outputSplitter

	got := splitter.Push([]byte("Console commands:\n  /login ...\n[bot-a] > "))

	assertOutputEvents(t, got, []string{"Console commands:", "  /login ..."}, "[bot-a] > ", true)
}

func TestOutputSplitterHandlesBroadcastPromptTail(t *testing.T) {
	var splitter outputSplitter

	got := splitter.Push([]byte("\n[Bot: a | Message from u]: hi\n> "))

	assertOutputEvents(t, got, []string{"", "[Bot: a | Message from u]: hi"}, "> ", true)
}

func TestOutputSplitterDoesNotPrependPreviousPromptToBroadcast(t *testing.T) {
	var splitter outputSplitter

	_ = splitter.Push([]byte("[bot-a] > "))
	got := splitter.Push([]byte("\n[Bot: a | Message from u]: hi\n> "))

	assertOutputEvents(t, got, []string{"", "[Bot: a | Message from u]: hi"}, "> ", true)
}

func TestOutputSplitterRecoversSplitPromptTail(t *testing.T) {
	var splitter outputSplitter

	first := splitter.Push([]byte("[bot-a"))
	second := splitter.Push([]byte("] > "))

	assertOutputEvents(t, first, nil, "[bot-a", true)
	assertOutputEvents(t, second, nil, "[bot-a] > ", true)
}

func TestOutputSplitterResetTailBeforeCommandResponse(t *testing.T) {
	var splitter outputSplitter

	_ = splitter.Push([]byte("[bot-a] > "))
	splitter.ResetTail()
	got := splitter.Push([]byte("Protection disabled.\n[bot-a] > "))

	assertOutputEvents(t, got, []string{"Protection disabled."}, "[bot-a] > ", true)
}

func TestOutputSplitterIgnoresEmptyInput(t *testing.T) {
	var splitter outputSplitter

	got := splitter.Push(nil)

	assertOutputEvents(t, got, nil, "", false)
}

func assertOutputEvents(t *testing.T, got outputEvents, wantLines []string, wantPrompt string, wantPromptChanged bool) {
	t.Helper()

	if !reflect.DeepEqual(got.lines, wantLines) {
		t.Fatalf("lines = %#v, want %#v", got.lines, wantLines)
	}
	if got.prompt != wantPrompt {
		t.Fatalf("prompt = %q, want %q", got.prompt, wantPrompt)
	}
	if got.promptChanged != wantPromptChanged {
		t.Fatalf("promptChanged = %v, want %v", got.promptChanged, wantPromptChanged)
	}
}
