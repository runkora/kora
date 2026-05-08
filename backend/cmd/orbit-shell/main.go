package main

// orbit-shell is the Bash proxy binary set as $SHELL in the agent container.
// It checks $WORKSPACE/.sandbox/endpoint on every invocation:
//   - absent  → exec local bash with the given args
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

// parseCommand extracts the argument after -c, matching bash's own convention.
func parseCommand() string {
	args := os.Args[1:]
	for i, arg := range args {
		if arg == "-c" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "-c") {
			return arg[2:]
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
