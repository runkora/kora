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
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

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
func runRemote(endpoint string) {
	command := parseCommand()
	if command == "" {
		// No -c flag: interactive shell — fall back to local bash.
		runLocal()
		return
	}

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("orbit-shell: dial %s: %v", endpoint, err)
	}

	client := execpb.NewExecServiceClient(conn)
	req := &execpb.ExecuteRequest{
		Command:    command,
		WorkingDir: workingDir(),
	}

	stream, err := client.Execute(context.Background(), req)
	if err != nil {
		conn.Close() //nolint:errcheck
		log.Fatalf("orbit-shell: execute: %v", err)
	}

	exitCode := 0
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			conn.Close() //nolint:errcheck
			log.Fatalf("orbit-shell: recv: %v", err)
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

	conn.Close() //nolint:errcheck
	os.Exit(exitCode)
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
