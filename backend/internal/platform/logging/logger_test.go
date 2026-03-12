package logging

import (
	"context"
	"testing"
)

// TestFieldCreation verifies that Field structs are created correctly
func TestFieldCreation(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   any
		wantKey string
		wantVal any
	}{
		{
			name:    "string field",
			key:     "username",
			value:   "testuser",
			wantKey: "username",
			wantVal: "testuser",
		},
		{
			name:    "int field",
			key:     "count",
			value:   42,
			wantKey: "count",
			wantVal: 42,
		},
		{
			name:    "bool field",
			key:     "enabled",
			value:   true,
			wantKey: "enabled",
			wantVal: true,
		},
		{
			name:    "nil value",
			key:     "optional",
			value:   nil,
			wantKey: "optional",
			wantVal: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := Field{Key: tt.key, Value: tt.value}

			if field.Key != tt.wantKey {
				t.Errorf("Field.Key = %v, want %v", field.Key, tt.wantKey)
			}

			if field.Value != tt.wantVal {
				t.Errorf("Field.Value = %v, want %v", field.Value, tt.wantVal)
			}
		})
	}
}

// TestLoggerInterface verifies that the Logger interface is properly defined
// This is a compile-time check that ensures implementations match the interface
func TestLoggerInterface(t *testing.T) {
	// This test primarily exists to ensure the interface compiles correctly
	// and that any implementation must provide all required methods
	var _ Logger = (*zerologLogger)(nil)
}

// mockLogger is a test double for testing code that depends on Logger
type mockLogger struct {
	debugCalls []logCall
	infoCalls  []logCall
	warnCalls  []logCall
	errorCalls []logCall
}

type logCall struct {
	ctx    context.Context
	msg    string
	fields []Field
}

func (m *mockLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	m.debugCalls = append(m.debugCalls, logCall{ctx, msg, fields})
}

func (m *mockLogger) Info(ctx context.Context, msg string, fields ...Field) {
	m.infoCalls = append(m.infoCalls, logCall{ctx, msg, fields})
}

func (m *mockLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	m.warnCalls = append(m.warnCalls, logCall{ctx, msg, fields})
}

func (m *mockLogger) Error(ctx context.Context, msg string, fields ...Field) {
	m.errorCalls = append(m.errorCalls, logCall{ctx, msg, fields})
}

// TestMockLogger verifies that our mock logger correctly captures calls
func TestMockLogger(t *testing.T) {
	mock := &mockLogger{}
	ctx := context.Background()

	// Test Debug
	mock.Debug(ctx, "debug message", Field{Key: "key", Value: "value"})
	if len(mock.debugCalls) != 1 {
		t.Errorf("Debug calls = %d, want 1", len(mock.debugCalls))
	}
	if mock.debugCalls[0].msg != "debug message" {
		t.Errorf("Debug message = %s, want 'debug message'", mock.debugCalls[0].msg)
	}

	// Test Info
	mock.Info(ctx, "info message")
	if len(mock.infoCalls) != 1 {
		t.Errorf("Info calls = %d, want 1", len(mock.infoCalls))
	}

	// Test Warn
	mock.Warn(ctx, "warn message")
	if len(mock.warnCalls) != 1 {
		t.Errorf("Warn calls = %d, want 1", len(mock.warnCalls))
	}

	// Test Error
	mock.Error(ctx, "error message")
	if len(mock.errorCalls) != 1 {
		t.Errorf("Error calls = %d, want 1", len(mock.errorCalls))
	}
}
