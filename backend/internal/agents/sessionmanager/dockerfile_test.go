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
