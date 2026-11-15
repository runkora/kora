package logging

import (
	"context"
	"testing"
)

type recordedLine struct {
	level, msg string
	fields     []Field
}

type recordingLogger struct{ lines []recordedLine }

func (l *recordingLogger) Debug(_ context.Context, msg string, f ...Field) {
	l.lines = append(l.lines, recordedLine{"debug", msg, f})
}
func (l *recordingLogger) Info(_ context.Context, msg string, f ...Field) {
	l.lines = append(l.lines, recordedLine{"info", msg, f})
}
func (l *recordingLogger) Warn(_ context.Context, msg string, f ...Field) {
	l.lines = append(l.lines, recordedLine{"warn", msg, f})
}
func (l *recordingLogger) Error(_ context.Context, msg string, f ...Field) {
	l.lines = append(l.lines, recordedLine{"error", msg, f})
}

func TestSlogLogger(t *testing.T) {
	rec := &recordingLogger{}
	log := NewSlogLogger(rec).With("run_id", "r1")
	log.Info("connection closed", "cause", "peer connection closed")
	log.Error("failed to parse incoming message", "raw", "x")
	log.Debug("dropped below the sink's verbosity")

	if len(rec.lines) != 2 {
		t.Fatalf("lines = %+v", rec.lines)
	}
	info, errLine := rec.lines[0], rec.lines[1]
	if info.level != "info" || info.msg != "connection closed" || len(info.fields) != 2 ||
		info.fields[0] != (Field{Key: "run_id", Value: "r1"}) || info.fields[1] != (Field{Key: "cause", Value: "peer connection closed"}) {
		t.Errorf("info line = %+v", info)
	}
	if errLine.level != "error" || errLine.fields[1] != (Field{Key: "raw", Value: "x"}) {
		t.Errorf("error line = %+v", errLine)
	}
}
