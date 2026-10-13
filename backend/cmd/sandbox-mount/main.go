//go:build linux

// Command sandbox-mount is the node agent that shares each sandbox's local
// folder with its session-manager pod (docs/adr/2026-10-08-sandbox-workspace-mount.md).
//
// It runs privileged on every node in the host network. On a sandbox's node it
// exports the sandbox folder with the kernel NFS server. On the session-manager's
// node it mounts that export under the session-manager pod's workspace volume,
// and the mount propagates into the running container.
//
// NODE_NAME is this node's name, set by the chart from the downward API.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	sandboxMountConfig "github.com/clear-route/orbit/cmd/sandbox-mount/config"
	"github.com/clear-route/orbit/internal/agents/sandbox/mount"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// errBadPort reports a configured nfsd port outside the TCP range.
var errBadPort = errors.New("not a TCP port")

func main() {
	os.Exit(mainRun())
}

func mainRun() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := logging.NewLogger()
	if err := run(ctx, sandboxMountConfig.Current, logger); err != nil {
		logger.Error(ctx, "sandbox-mount stopped", logging.Field{Key: "error", Value: err.Error()})
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg *sandboxMountConfig.Config, logger logging.Logger) error {
	node := os.Getenv("NODE_NAME")
	if node == "" {
		return errors.New("NODE_NAME must be set")
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("in-cluster config: %w", err)
	}
	kube, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	port := cfg.Server.Port
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: server.port %d", errBadPort, port)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server, mountd, err := mount.StartServer(ctx, mount.ServerConfig{
		KubeletDir:  cfg.Node.KubeletDir,
		ExportRoot:  cfg.Server.ExportRoot,
		ExportsFile: cfg.Server.ExportsFile,
		Threads:     cfg.Server.Threads,
		GraceSecs:   cfg.Server.GraceSecs,
		Port:        port,
		TrackingDir: cfg.Server.TrackingDir,
	})
	if err != nil {
		return fmt.Errorf("start NFS server: %w", err)
	}
	agentCfg := mount.Config{Node: node, KubeletDir: cfg.Node.KubeletDir, Resync: cfg.Resync(), Retry: cfg.Retry(), LedgerFile: cfg.Node.LedgerFile, Port: port}
	client := mount.Client{KubeletDir: cfg.Node.KubeletDir, HostKubeletDir: cfg.Node.HostKubeletDir, Port: port, Options: cfg.Client.MountOptions}
	agent, err := mount.NewAgent(agentCfg, kube, server, client, logger)
	if err != nil {
		return err
	}
	logger.Info(ctx, "sandbox-mount started", logging.Field{Key: "node", Value: node})

	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	select {
	case err := <-mountd:
		if ctx.Err() != nil {
			return nil
		}
		// The agent cannot serve exports without mountd and nfsdcld. Exiting restarts the container.
		return fmt.Errorf("NFS daemon exited: %w", err)
	case err := <-done:
		return err
	}
}
