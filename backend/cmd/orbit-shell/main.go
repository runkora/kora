package main

// orbit-shell is the Bash proxy that routes the agent's command execution into the
// per-run sandbox pod. It is wired in as Claude Code's shell via CLAUDE_CODE_SHELL.
//
// Wiring note: Claude Code IGNORES $SHELL and resolves its Bash-tool shell from
// CLAUDE_CODE_SHELL, which it only honors when the path's basename is a known shell
// (bash/zsh/fish) — any other basename (e.g. "orbit-shell") is silently rejected and
// Claude falls back to /bin/bash. So the deploy image exposes this binary under a
// "bash" basename symlink (/opt/orbit/shell/bash) and sets CLAUDE_CODE_SHELL to it.
// This was validated empirically against the real claude binary.
//
// It checks $WORKSPACE/.sandbox/endpoint on every invocation:
//   - absent  → exec the real local /bin/bash with the given args (snapshot generation
//     and any pre-provision commands run in-pod)
//   - present → dial the gRPC exec server in the sandbox pod and stream the command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	execpb "github.com/clear-route/orbit/internal/agents/sandbox/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	endpoint := sandboxEndpoint()

	if endpoint == "" {
		runLocal()
		return
	}

	runRemote(endpoint)
}

// sandboxEndpoint reads the sandbox gRPC address from the workspace endpoint
// file. Returns empty string if the file does not exist (no sandbox yet).
func sandboxEndpoint() string {
	workspace := os.Getenv("WORKSPACE")
	if workspace == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(workspace, ".sandbox", "endpoint"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// runLocal execs the real bash in place of this process, preserving all args.
func runLocal() {
	bash, err := exec.LookPath("bash")
	if err != nil {
		log.Fatalf("orbit-shell: bash not found: %v", err)
	}
	// Replace this process with bash — exit code propagates naturally.
	if err := syscall.Exec(bash, append([]string{"bash"}, os.Args[1:]...), os.Environ()); err != nil { //nolint:gosec
		log.Fatalf("orbit-shell: exec bash: %v", err)
	}
}

// runRemote streams the command to the sandbox gRPC exec server.
//
// If the sandbox is unreachable (the pod died/was evicted, leaving a stale
// endpoint), execOne returns an infra error. Rather than failing the command —
// which would wedge the run, since the local-bash fallback only applies when no
// sandbox is expected — we ask the session-manager to re-provision the sandbox,
// re-read the freshly written endpoint, and retry once. The endpoint workspace
// files survive on the shared volume, so a replacement sandbox resumes against
// the same workspace. A normal non-zero command exit is NOT an infra error and
// never triggers a retry.
func runRemote(endpoint string) {
	command := parseCommand()
	if command == "" {
		// No -c flag: interactive shell — fall back to local bash.
		runLocal()
		return
	}

	exitCode, err := execOne(endpoint, command)
	if err == nil {
		os.Exit(exitCode)
	}

	// Sandbox unreachable — attempt one re-provision + retry.
	newEndpoint, reErr := reprovisionSandbox()
	if reErr != nil {
		log.Fatalf("orbit-shell: sandbox unreachable (%v) and re-provision failed: %v", err, reErr)
	}
	exitCode, err = execOne(newEndpoint, command)
	if err != nil {
		log.Fatalf("orbit-shell: sandbox unreachable after re-provision: %v", err)
	}
	os.Exit(exitCode)
}

// execOne dials the sandbox and streams a single command. It returns the
// command's exit code on success. It returns a non-nil error ONLY for
// infrastructure failures (dial/Execute/Recv) that indicate the sandbox is
// unreachable — a command that runs and exits non-zero is a success here, with
// its code returned via exitCode.
func execOne(endpoint, command string) (exitCode int, err error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, fmt.Errorf("dial %s: %w", endpoint, err)
	}
	defer conn.Close() //nolint:errcheck

	client := execpb.NewExecServiceClient(conn)
	req := &execpb.ExecuteRequest{
		Command:    command,
		WorkingDir: workingDir(),
	}

	stream, err := client.Execute(context.Background(), req)
	if err != nil {
		return 0, fmt.Errorf("execute: %w", err)
	}

	for {
		resp, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return 0, fmt.Errorf("recv: %w", recvErr)
		}
		switch v := resp.Output.(type) {
		case *execpb.ExecuteResponse_Stdout:
			os.Stdout.Write(v.Stdout) //nolint:errcheck
		case *execpb.ExecuteResponse_Stderr:
			os.Stderr.Write(v.Stderr) //nolint:errcheck
		case *execpb.ExecuteResponse_ExitCode:
			exitCode = int(v.ExitCode)
		}
	}
	return exitCode, nil
}

// reprovisionSandbox asks the session-manager to (re)provision the run's sandbox
// and returns the fresh gRPC endpoint. The session-manager liveness-checks the
// existing endpoint and replaces a dead sandbox, then rewrites
// $WORKSPACE/.sandbox/endpoint, which we re-read here.
func reprovisionSandbox() (string, error) {
	smURL := os.Getenv("ORBIT_SESSION_MANAGER_URL")
	runID := os.Getenv("ORBIT_RUN_ID")
	if smURL == "" || runID == "" {
		return "", fmt.Errorf("missing ORBIT_SESSION_MANAGER_URL/ORBIT_RUN_ID")
	}

	url := fmt.Sprintf("%s/internal/session/%s/provision-sandbox", smURL, runID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("build provision request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post provision-sandbox: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("provision-sandbox returned %d", resp.StatusCode)
	}

	endpoint := sandboxEndpoint()
	if endpoint == "" {
		return "", fmt.Errorf("endpoint still empty after re-provision")
	}
	return endpoint, nil
}

// parseCommand extracts the command string bash would run for a `-c` invocation.
//
// It must match bash's real option grammar, not just "the arg after -c". bash parses
// ALL leading options first, then takes the FIRST non-option operand as the command
// string. Claude Code invokes its shell as `bash -c -l "<command>"` (and variants like
// `bash -c -l "SNAPSHOT_FILE=…; …"`), so the command is the first operand — NOT the arg
// immediately after `-c`, which is the `-l` flag. Getting this wrong shipped "-l" to the
// sandbox and produced `/bin/bash: -c: option requires an argument`.
//
// Returns "" when there is no `-c` (interactive or non-command invocation) so the caller
// falls back to local bash.
func parseCommand() string {
	return parseCommandArgs(os.Args[1:])
}

// parseCommandArgs is the testable core of parseCommand; args excludes argv[0].
func parseCommandArgs(args []string) string {
	commandMode := false
	endOfOptions := false
	for _, arg := range args {
		switch {
		case endOfOptions:
			// First operand after `--`.
			if commandMode {
				return arg
			}
			return ""
		case arg == "--":
			endOfOptions = true
		case strings.HasPrefix(arg, "--"):
			// Long option (e.g. --norc, --login) — never the -c command flag.
			continue
		case strings.HasPrefix(arg, "-"):
			// Short-option bundle (e.g. -c, -l, -lc). A 'c' selects command mode.
			if strings.ContainsRune(arg, 'c') {
				commandMode = true
			}
		default:
			// First non-option operand: this is the command string in -c mode.
			if commandMode {
				return arg
			}
			return ""
		}
	}
	return ""
}

func workingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "orbit-shell: getwd: %v\n", err)
	}
	return wd
}
