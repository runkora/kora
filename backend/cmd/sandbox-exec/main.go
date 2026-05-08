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
	"syscall"

	execpb "github.com/clear-route/orbit/internal/agents/sandbox/proto"
	"google.golang.org/grpc"
)

func main() {
	port := os.Getenv("SANDBOX_EXEC_PORT")
	if port == "" {
		port = "50051"
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
