package operatorConfig

import "testing"

// Malformed JSON is a startup error. Valid JSON parses and an empty variable is the zero value.
func TestJSONEnv(t *testing.T) {
	StartupErrors = nil
	t.Setenv("TEST_MIRRORS", `{"orbit-registry:5001":"http://orbit-registry:5000"}`)
	if got := jsonEnv[map[string]string]("TEST_MIRRORS"); got["orbit-registry:5001"] != "http://orbit-registry:5000" {
		t.Errorf("mirrors = %v", got)
	}
	if got := jsonEnv[map[string]string]("TEST_UNSET"); got != nil || len(StartupErrors) != 0 {
		t.Errorf("unset = %v, errors %v", got, StartupErrors)
	}
	t.Setenv("TEST_BAD", "orbit-registry:5001=orbit-registry:5000")
	jsonEnv[map[string]string]("TEST_BAD")
	if len(StartupErrors) != 1 {
		t.Errorf("errors = %v, want one for malformed JSON", StartupErrors)
	}
	StartupErrors = nil
}
