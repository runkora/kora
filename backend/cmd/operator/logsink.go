package main

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// devEnvLogSinkVerbosityCutoff is the logr V-level at or above which the sink
// is disabled. controller-runtime's own internals log routine reconcile noise
// at V(5)+ and very chatty client-go transport detail beyond that; anything
// at or above this cutoff is dropped before it reaches logging.Logger.
const devEnvLogSinkVerbosityCutoff = 8

// crLogSink adapts controller-runtime's logr.LogSink to this repo's injected
// logging.Logger, so manager/reconciler-internal logs (leader election,
// webhook server, cache sync, reconcile panics recovered by the runtime) are
// no longer silently discarded via logr.Discard() — a prior gap that hid
// reconcile failures with no operator-side signal.
//
// logr.LogSink methods carry no context.Context; logging.Logger requires one
// as its first argument for trace/span correlation, so calls here use
// context.Background() rather than a request-scoped context.
type crLogSink struct {
	logger logging.Logger
	name   string
	values []logging.Field
}

// newCRLogSink constructs a logr.LogSink backed by logger.
func newCRLogSink(logger logging.Logger) logr.LogSink {
	return &crLogSink{logger: logger}
}

func (s *crLogSink) Init(_ logr.RuntimeInfo) {}

func (s *crLogSink) Enabled(level int) bool {
	return level < devEnvLogSinkVerbosityCutoff
}

func (s *crLogSink) Info(_ int, msg string, keysAndValues ...any) {
	s.logger.Info(context.Background(), s.formatMsg(msg), s.fieldsFor(keysAndValues)...)
}

func (s *crLogSink) Error(err error, msg string, keysAndValues ...any) {
	fields := append(s.fieldsFor(keysAndValues), logging.Field{Key: "error", Value: err.Error()})
	s.logger.Error(context.Background(), s.formatMsg(msg), fields...)
}

func (s *crLogSink) WithValues(keysAndValues ...any) logr.LogSink {
	values := append(append([]logging.Field{}, s.values...), s.fieldsFor(keysAndValues)...)
	return &crLogSink{logger: s.logger, name: s.name, values: values}
}

func (s *crLogSink) WithName(name string) logr.LogSink {
	newName := name
	if s.name != "" {
		newName = s.name + "." + name
	}
	return &crLogSink{logger: s.logger, name: newName, values: s.values}
}

// formatMsg prefixes msg with the accumulated WithName chain, mirroring how
// logr's own sinks render nested component names (e.g. "controller-runtime.manager").
func (s *crLogSink) formatMsg(msg string) string {
	if s.name == "" {
		return msg
	}
	return s.name + ": " + msg
}

// fieldsFor prepends any WithValues-accumulated fields to the keysAndValues
// pairs passed to this call, converting non-string keys via fmt.Sprintf as
// logr itself tolerates non-string keys from callers.
func (s *crLogSink) fieldsFor(keysAndValues []any) []logging.Field {
	fields := make([]logging.Field, 0, len(s.values)+len(keysAndValues)/2)
	fields = append(fields, s.values...)
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			key = fmt.Sprintf("%v", keysAndValues[i])
		}
		fields = append(fields, logging.Field{Key: key, Value: keysAndValues[i+1]})
	}
	return fields
}
