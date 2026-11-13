package connections

import (
	"regexp"
	"strings"
)

// placeholderEnvNameRE is the shape of a sandbox env var name: a plain
// identifier of at most 128 characters.
var placeholderEnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ValidPlaceholderEnvName reports whether name is safe to set in a sandbox as
// an env var holding the credential placeholder (an apikey connection's
// config.placeholderEnv): a plain identifier that is not a loader or
// process-critical variable. It is the single rule shared by Connection
// admission and the sandbox runtime, so admission refuses everything the
// runtime would refuse.
func ValidPlaceholderEnvName(name string) bool {
	if !placeholderEnvNameRE.MatchString(name) {
		return false
	}
	u := strings.ToUpper(name)
	switch u {
	case "PATH", "HOME", "USER", "SHELL", "PWD", "IFS", "ENV", "BASH_ENV", "PYTHONPATH", "NODE_OPTIONS", "KUBECONFIG":
		return false
	}
	return !strings.HasPrefix(u, "LD_") && !strings.HasPrefix(u, "DYLD_") && !strings.HasPrefix(u, "KORA_")
}
