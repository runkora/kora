package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/clear-route/orbit/internal/agents/sandbox/execauth"
	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
	execpb "github.com/clear-route/orbit/internal/agents/sandbox/proto"
	"github.com/clear-route/orbit/internal/agents/sandbox/runfs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	port := os.Getenv("SANDBOX_EXEC_PORT")
	if port == "" {
		port = "50051"
	}

	// HOME, TMPDIR and caches go to this sandbox's local disk, off the shared workspace volume
	// (docs/adr/2026-10-08-sandbox-workspace-mount.md).
	if err := setRunHome(); err != nil {
		// Not fatal: HOME stays at the image's home, a pod-local scratch volume.
		// The workspace SubPath mount is the isolation boundary, and this fallback does not touch it.
		log.Printf("sandbox-exec: run home not set, falling back to default HOME: %v", err)
	}

	// Materialize the kubeconfig for whichever Kubernetes connections this agent
	// is granted, before any command can run. It holds no credential — the egress
	// gateway injects the brokered ServiceAccount token on the wire — so the agent
	// runs `kubectl get pods` with no flags, no server URL and no token, exactly
	// as `gh` works via the GH_TOKEN placeholder.
	if err := writeKubeconfig(); err != nil {
		// Not fatal: an agent with no Kubernetes connection must still start, and
		// one with a broken kubeconfig should fail at kubectl, not at boot.
		log.Printf("sandbox-exec: kubeconfig not written: %v", err)
	}

	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	// The exec port is outside the inbound mesh, so no sidecar authenticates the
	// caller. This token is what distinguishes this run from its siblings.
	execToken := os.Getenv(execauth.TokenEnv)
	if execToken == "" {
		log.Fatalf("%s is not set: refusing to serve an unauthenticated exec server", execauth.TokenEnv)
	}
	if err := hideEnviron(); err != nil {
		log.Fatalf("hide environ: %v", err)
	}

	srv := grpc.NewServer(
		grpc.StreamInterceptor(requireExecTokenStream(execToken)),
		grpc.UnaryInterceptor(requireExecTokenUnary(execToken)),
	)
	execpb.RegisterExecServiceServer(srv, &execServer{})

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		srv.GracefulStop()
	}()

	log.Printf("sandbox-exec listening on :%s", port) //nolint:gosec
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

var errUnauthenticated = status.Error(codes.Unauthenticated, "missing or invalid sandbox exec token")

// requireExecToken reports whether the call presented the run's token. Constant
// time, since a caller can retry freely.
func requireExecToken(ctx context.Context, want []byte) bool {
	got, ok := execauth.TokenFrom(ctx)
	return ok && subtle.ConstantTimeCompare([]byte(got), want) == 1
}

// requireExecTokenStream guards the streaming surface.
func requireExecTokenStream(want string) grpc.StreamServerInterceptor {
	wantBytes := []byte(want)
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !requireExecToken(ss.Context(), wantBytes) {
			return errUnauthenticated
		}
		return handler(srv, ss)
	}
}

// requireExecTokenUnary guards the unary surface. ExecService has no unary RPC
// today; this is here so adding one cannot land unauthenticated.
func requireExecTokenUnary(want string) grpc.UnaryServerInterceptor {
	wantBytes := []byte(want)
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !requireExecToken(ctx, wantBytes) {
			return nil, errUnauthenticated
		}
		return handler(ctx, req)
	}
}

type execServer struct {
	execpb.UnimplementedExecServiceServer
}

func (s *execServer) Execute(req *execpb.ExecuteRequest, stream grpc.ServerStreamingServer[execpb.ExecuteResponse]) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.CommandContext(stream.Context(), shell, "-c", req.Command) //nolint:gosec
	if req.WorkingDir != "" {
		if _, err := os.Stat(req.WorkingDir); err == nil {
			cmd.Dir = req.WorkingDir
		}
	}
	cmd.Env = append(commandEnv(os.Environ()), filterEnv(req.Env)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	// stream stdout and stderr concurrently into the response stream
	done := make(chan error, 2)
	go streamPipe(stream, stdout, func(b []byte) *execpb.ExecuteResponse {
		return &execpb.ExecuteResponse{Output: &execpb.ExecuteResponse_Stdout{Stdout: b}}
	}, done)
	go streamPipe(stream, stderr, func(b []byte) *execpb.ExecuteResponse {
		return &execpb.ExecuteResponse{Output: &execpb.ExecuteResponse_Stderr{Stderr: b}}
	}, done)

	// wait for both pipe readers to finish
	for range 2 {
		if err := <-done; err != nil {
			return err
		}
	}

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return err
		}
	}

	return stream.Send(&execpb.ExecuteResponse{
		Output: &execpb.ExecuteResponse_ExitCode{ExitCode: int32(exitCode)},
	})
}

// deniedEnvPrefixes covers families of variables that can hijack subprocess
// behavior (dynamic linker/loader, memory allocator) regardless of the exact
// suffix, so new members of a family are blocked without an allowlist update.
var deniedEnvPrefixes = []string{"LD_", "MALLOC_", "DYLD_"}

// deniedEnvNames covers individual variables that can redirect executable
// lookup or swap the interpreter used to run commands.
var deniedEnvNames = map[string]bool{
	"PATH":            true,
	"SHELL":           true,
	"IFS":             true,
	"ENV":             true,
	"BASH_ENV":        true,
	"PS4":             true,
	"CDPATH":          true,
	"GCONV_PATH":      true,
	"NSS_WRAPPER_LIB": true,
}

// serverOnlyEnv is sandbox-exec's own configuration, kept from the commands it runs.
// Anyone holding the exec token can run commands in this sandbox, so a command must not be able to read it.
var serverOnlyEnv = []string{execauth.TokenEnv, "SANDBOX_EXEC_PORT"}

// commandEnv is the pod environment a command runs with, minus serverOnlyEnv.
func commandEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(e string) bool {
		key, _, _ := strings.Cut(e, "=")
		return slices.Contains(serverOnlyEnv, key)
	})
}

// filterEnv drops entries from req.Env that would let a caller hijack the
// subprocess (e.g. LD_PRELOAD, PATH, SHELL) rather than merely configure it,
// while still allowing arbitrary safe additions (e.g. WORKSPACE, ANTHROPIC_*).
// See https://github.com/clear-route/orbit/issues/231.
func filterEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		upperKey := strings.ToUpper(key)
		if deniedEnvNames[upperKey] {
			continue
		}
		denied := false
		for _, prefix := range deniedEnvPrefixes {
			if strings.HasPrefix(upperKey, prefix) {
				denied = true
				break
			}
		}
		if denied {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

func streamPipe(stream grpc.ServerStreamingServer[execpb.ExecuteResponse], r interface{ Read([]byte) (int, error) }, wrap func([]byte) *execpb.ExecuteResponse, done chan<- error) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(wrap(chunk)); sendErr != nil {
				done <- sendErr
				return
			}
		}
		if err != nil {
			break
		}
	}
	done <- nil
}

// sandboxTmpDir is the sandbox's TMPDIR. The pod mounts the local disk there.
const sandboxTmpDir = "/tmp"

// setRunHome points HOME, TMPDIR, installs and caches at the sandbox's local disk.
// It does nothing when WORKSPACE is unset (local/dev invocation).
func setRunHome() error {
	workspace := os.Getenv("WORKSPACE")
	if workspace == "" {
		return nil
	}
	home := runfs.LocalDir(workspace)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create run home: %w", err)
	}
	env := append([]string{"HOME=" + home, "TMPDIR=" + sandboxTmpDir}, runfs.InstallEnv(home, os.Getenv("PATH"))...)
	for _, e := range env {
		key, value, _ := strings.Cut(e, "=")
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return nil
}

// kubeconfigPath is where the rendered kubeconfig lands. kubectl reads
// $HOME/.kube/config by default, so writing here means the agent needs no
// KUBECONFIG env var and no --kubeconfig flag.
const kubeconfigPath = ".kube/config"

// writeKubeconfig renders the credential-less kubeconfig for the Kubernetes
// connections named in ORBIT_KUBE_CLUSTERS. It is a no-op when the agent holds
// no Kubernetes connection.
func writeKubeconfig() error {
	clusters, err := kubeconfig.Decode(os.Getenv(kubeconfig.ClustersEnvVar))
	if err != nil {
		return err
	}
	if len(clusters) == 0 {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	path := filepath.Join(home, kubeconfigPath)
	return kubeconfig.WriteTo(path, clusters)
}
