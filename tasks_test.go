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

func TestCommandOutputWriterSuppressesFFmpegProgressLogs(t *testing.T) {
	var logs strings.Builder
	writer := &commandOutputWriter{
		logger: newCustomLogger(log.New(&logs, "", 0), ""),
	}

	progress := "frame= 1671 fps=2.1 q=25.0 size=  721920KiB time=00:00:55.72 bitrate=106132.8kbits/s speed=0.0711x"
	_, _ = writer.Write([]byte("encoding started\n" + progress + "\rwarning: useful message\n"))
	writer.Flush()

	if !strings.Contains(writer.String(), progress) {
		t.Fatal("captured output should keep ffmpeg progress for command failure details")
	}
	if strings.Contains(logs.String(), "frame= 1671") {
		t.Fatalf("ffmpeg progress line should not be streamed to logs: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "encoding started") || !strings.Contains(logs.String(), "warning: useful message") {
		t.Fatalf("non-progress output should still be streamed: %s", logs.String())
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
	processor.SetEnvironment("IUO_VIDEO_CRF=28")

	if len(processor.environment) != 4 || processor.environment[0] != "IUO_USE_NVIDIA=1" || processor.environment[1] != "IUO_IMAGE_SCORE=85" || processor.environment[2] != "IUO_VIDEO_SCORE=95" || processor.environment[3] != "IUO_VIDEO_CRF=28" {
		t.Fatalf("unexpected task environment: %v", processor.environment)
	}
}
