package operator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// logSink adapts logging.Logger to controller-runtime's logr interface, so the
// errors controller-runtime logs on the operator's behalf (a Reconcile
// returning an error, webhook server TLS failures) land in the same structured
// log stream as everything else. Without it they are dropped.
type logSink struct {
	log    logging.Logger
	name   string
	fields []logging.Field
}

// NewLogSink wraps logger for ctrl.SetLogger.
func NewLogSink(logger logging.Logger) logr.LogSink {
	return &logSink{log: logger}
}

func (s *logSink) Init(logr.RuntimeInfo) {}

// Enabled admits V(0) and V(1); controller-runtime's deeper verbosity is noise
// at Orbit's log volume.
func (s *logSink) Enabled(level int) bool { return level <= 1 }

func (s *logSink) Info(level int, msg string, kv ...any) {
	fields := s.with(kv)
	if level == 0 {
		s.log.Info(context.Background(), s.prefixed(msg), fields...)
		return
	}
	s.log.Debug(context.Background(), s.prefixed(msg), fields...)
}

func (s *logSink) Error(err error, msg string, kv ...any) {
	fields := s.with(kv)
	if err != nil {
		fields = append(fields, logging.Field{Key: "error", Value: err.Error()})
	}
	s.log.Error(context.Background(), s.prefixed(msg), fields...)
}

func (s *logSink) WithValues(kv ...any) logr.LogSink {
	return &logSink{log: s.log, name: s.name, fields: s.with(kv)}
}

func (s *logSink) WithName(name string) logr.LogSink {
	if s.name != "" {
		name = s.name + "." + name
	}
	return &logSink{log: s.log, name: name, fields: s.fields}
}

func (s *logSink) prefixed(msg string) string {
	if s.name == "" {
		return msg
	}
	return s.name + ": " + msg
}

// with returns the sink's fields plus kv folded into key/value pairs; a
// trailing key with no value is kept with an empty value rather than dropped.
func (s *logSink) with(kv []any) []logging.Field {
	fields := make([]logging.Field, 0, len(s.fields)+len(kv)/2+1)
	fields = append(fields, s.fields...)
	for i := 0; i < len(kv); i += 2 {
		key := fmt.Sprint(kv[i])
		var val any
		if i+1 < len(kv) {
			val = kv[i+1]
		}
		fields = append(fields, logging.Field{Key: key, Value: val})
	}
	return fields
}
