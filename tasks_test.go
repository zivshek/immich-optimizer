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

func TestTaskProcessorAccumulatesEnvironment(t *testing.T) {
	processor := &TaskProcessor{}
	processor.SetEnvironment("IUO_USE_NVIDIA=1")
	processor.SetEnvironment("IUO_IMAGE_SCORE=85")
	processor.SetEnvironment("IUO_VIDEO_SCORE=95")

	if len(processor.environment) != 3 || processor.environment[0] != "IUO_USE_NVIDIA=1" || processor.environment[1] != "IUO_IMAGE_SCORE=85" || processor.environment[2] != "IUO_VIDEO_SCORE=95" {
		t.Fatalf("unexpected task environment: %v", processor.environment)
	}
}
