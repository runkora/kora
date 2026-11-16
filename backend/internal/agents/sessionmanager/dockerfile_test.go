package sessionmanager

import (
	"os"
	"regexp"
	"testing"
)

// OpenCode's grep, glob and list tools need ripgrep and download it from GitHub
// when none is on PATH. The agent pod cannot reach GitHub, so the image must ship it.
func TestSessionManagerImageShipsRipgrep(t *testing.T) {
	data, err := os.ReadFile("../../../Dockerfile.session-manager")
	if err != nil {
		t.Fatalf("read Dockerfile.session-manager: %v", err)
	}
	if !regexp.MustCompile(`(?m)^RUN apk add [^\n]*\bripgrep\b`).Match(data) {
		t.Error("Dockerfile.session-manager runtime stage must apk add ripgrep so OpenCode does not download rg")
	}
}

// The container runs ./kora, and harness children orphaned when a harness is killed reparent to
// PID 1. Only an init reaps them, so the image must run kora under tini or zombies pile up.
func TestSessionManagerImageRunsUnderTini(t *testing.T) {
	data, err := os.ReadFile("../../../Dockerfile.session-manager")
	if err != nil {
		t.Fatalf("read Dockerfile.session-manager: %v", err)
	}
	if !regexp.MustCompile(`(?m)^RUN apk add [^\n]*\btini\b`).Match(data) {
		t.Error("Dockerfile.session-manager runtime stage must apk add tini")
	}
	if !regexp.MustCompile(`(?m)^ENTRYPOINT \["/sbin/tini", "--"\]$`).Match(data) {
		t.Error(`Dockerfile.session-manager must set ENTRYPOINT ["/sbin/tini", "--"] (without -g, so signals reach only ./kora)`)
	}
	if !regexp.MustCompile(`(?m)^CMD \["\./kora"\]$`).Match(data) {
		t.Error(`Dockerfile.session-manager must keep CMD ["./kora"]`)
	}
}
