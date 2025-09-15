package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	machine "github.com/tinyrange/tinyrange/vibe_party/machine"
	machinepb "github.com/tinyrange/tinyrange/vibe_party/machine/proto"
	"google.golang.org/grpc"
)

func appMain() error {
	addr := os.Getenv("MACHINE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:50051"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()

	srv := grpc.NewServer()
	machinepb.RegisterMachineServiceServer(srv, machine.NewLocalMachineServer())

	slog.Info("machine server listening", "addr", addr)

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	done := make(chan error, 1)
	go func() { done <- srv.Serve(lis) }()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		srv.GracefulStop()
		return nil
	case err := <-done:
		return err
	}
}

func main() {
	if err := appMain(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
