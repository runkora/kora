package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
	execpb "github.com/clear-route/orbit/internal/agents/sandbox/proto"
	"google.golang.org/grpc"
)

func main() {
	port := os.Getenv("SANDBOX_EXEC_PORT")
	if port == "" {
		port = "50051"
	}

	// Per-run HOME: this run's sandbox pod is mounted with SubPath: runID (see
	// internal/agents/k8s.go sandboxContainer), scoping WORKSPACE to this run's
	// own directory. Mirroring orbit-session-runner's {workDir}/.runhome
	// (docs/adr/2026-06-04-per-session-home-isolation.md), overriding HOME here
	// keeps kubeconfig and tool caches (npm, pip, go, etc.) inside that scoped
	// directory instead of the pod's shared default home.
	if err := setRunHome(); err != nil {
		// Not fatal: falls back to the container's default HOME. Isolation still
		// holds via the SubPath mount; this only affects cache placement.
		log.Printf("sandbox-exec: run home not set: %v", err)
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

	srv := grpc.NewServer()
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
	cmd.Env = append(os.Environ(), req.Env...)

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

// setRunHome points HOME at {WORKSPACE}/.runhome, so kubeconfig and anything
// tools write under $HOME lands inside this run's own SubPath-scoped
// workspace directory rather than the pod's shared default home. A no-op
// when WORKSPACE is unset (e.g. local/dev invocation outside a sandbox pod).
func setRunHome() error {
	workspace := os.Getenv("WORKSPACE")
	if workspace == "" {
		return nil
	}
	runHome := filepath.Join(workspace, ".runhome")
	if err := os.MkdirAll(runHome, 0o700); err != nil {
		return fmt.Errorf("create run home: %w", err)
	}
	return os.Setenv("HOME", runHome)
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
