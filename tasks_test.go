package main

import (
	"io"
	"log"
	"strings"
	"testing"
)

func TestCommandOutputWriterStreamsLogsAndCapturesOutput(t *testing.T) {
	var logs strings.Builder
	writer := &commandOutputWriter{
		logger: newCustomLogger(log.New(&logs, "", 0), ""),
	}

	_, _ = writer.Write([]byte("first line\nprogress 10%\rprogress 20%"))
	writer.Flush()

	if got := writer.String(); got != "first line\nprogress 10%\rprogress 20%" {
		t.Fatalf("unexpected captured output: %q", got)
	}
	for _, expected := range []string{"first line", "progress 10%", "progress 20%"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("streamed logs missing %q: %s", expected, logs.String())
		}
	}
}

func TestCommandOutputWriterWithoutLogger(t *testing.T) {
	writer := &commandOutputWriter{}
	if _, err := io.WriteString(writer, "output"); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
}
