package main

import (
	"context"
	"strings"

	"github.com/go-logr/logr"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// crLogSink adapts controller-runtime's logr.LogSink to the platform's
// logging.Logger. Wired in via ctrl.SetLogger so that errors
// controller-runtime logs internally — most importantly "Reconciler error"
// on every failed Reconcile call, for any controller in this binary — are no
// longer silently discarded (see the vcluster ADR's Next Steps item 2: a
// stuck OrbitDevEnvironment deletion produced 20 failed reconciles with zero
// log output while this was wired to logr.Discard()).
type crLogSink struct {
	logger logging.Logger
	names  []string
	values []any
}

func newCRLogSink(logger logging.Logger) logr.Logger {
	return logr.New(&crLogSink{logger: logger})
}

func (s *crLogSink) Init(_ logr.RuntimeInfo) {}

func (s *crLogSink) Enabled(_ int) bool { return true }

func (s *crLogSink) Info(_ int, msg string, keysAndValues ...any) {
	s.logger.Info(context.Background(), s.prefixed(msg), s.fields(keysAndValues, nil)...)
}

func (s *crLogSink) Error(err error, msg string, keysAndValues ...any) {
	s.logger.Error(context.Background(), s.prefixed(msg), s.fields(keysAndValues, err)...)
}

func (s *crLogSink) WithValues(keysAndValues ...any) logr.LogSink {
	return &crLogSink{
		logger: s.logger,
		names:  s.names,
		values: append(append([]any{}, s.values...), keysAndValues...),
	}
}

func (s *crLogSink) WithName(name string) logr.LogSink {
	return &crLogSink{
		logger: s.logger,
		names:  append(append([]string{}, s.names...), name),
		values: s.values,
	}
}

// prefixed mirrors logr's convention of qualifying messages with the logger
// name chain (e.g. "controller.orbitdevenvironment"), since our own Logger
// has no separate concept of a logger name.
func (s *crLogSink) prefixed(msg string) string {
	if len(s.names) == 0 {
		return msg
	}
	var name strings.Builder
	name.WriteString(s.names[0])
	for _, n := range s.names[1:] {
		name.WriteString("." + n)
	}
	return "[" + name.String() + "] " + msg
}

func (s *crLogSink) fields(keysAndValues []any, err error) []logging.Field {
	fields := make([]logging.Field, 0, len(s.values)/2+len(keysAndValues)/2+1)
	fields = appendPairs(fields, s.values)
	fields = appendPairs(fields, keysAndValues)
	if err != nil {
		fields = append(fields, logging.Field{Key: "error", Value: err.Error()})
	}
	return fields
}

func appendPairs(fields []logging.Field, kv []any) []logging.Field {
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		fields = append(fields, logging.Field{Key: key, Value: kv[i+1]})
	}
	return fields
}
