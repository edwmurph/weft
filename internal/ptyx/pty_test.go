package ptyx

import (
	"bytes"
	"testing"
)

func TestStripOSCPreservesCarriageReturns(t *testing.T) {
	clean, title := stripOSC([]byte("one\r\n\x1b]2;Ready\x07two\r\n"))

	if title != "Ready" {
		t.Fatalf("title = %q", title)
	}
	if !bytes.Equal(clean, []byte("one\r\ntwo\r\n")) {
		t.Fatalf("clean = %#v", clean)
	}
}

func TestAnswerColorRequestsInjectsTerminalDefaults(t *testing.T) {
	data, response := answerColorRequests([]byte("a\x1b]10;?\x1b\\b\x1b]11;?\x07c"))

	wantData := []byte("a" + defaultForegroundResponse + "b" + defaultBackgroundResponse + "c")
	wantResponse := []byte(defaultForegroundResponse + defaultBackgroundResponse)
	if !bytes.Equal(data, wantData) {
		t.Fatalf("terminal data = %#v, want %#v", data, wantData)
	}
	if !bytes.Equal(response, wantResponse) {
		t.Fatalf("response = %#v, want %#v", response, wantResponse)
	}
}
