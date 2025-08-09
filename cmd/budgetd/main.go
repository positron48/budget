package main

import (
    "context"
    "net"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/example/budget/internal/adapter/postgres"
    "github.com/example/budget/internal/pkg/config"
    "github.com/example/budget/internal/pkg/logger"

    "google.golang.org/grpc"
    health "google.golang.org/grpc/health"
    healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
    // Load config & logger
    cfg, err := config.Load()
    if err != nil {
        panic(err)
    }
    lg, err := logger.New(cfg.AppEnv)
    if err != nil {
        panic(err)
    }
    defer lg.Sync() //nolint:errcheck
    sug := lg.Sugar()

    // DB connect
    ctx := context.Background()
    var db *postgres.Pool
    if cfg.DatabaseURL != "" {
        db, err = postgres.NewPool(ctx, cfg.DatabaseURL)
        if err != nil {
            sug.Fatalw("db connect failed", "error", err)
        }
        if pingErr := db.Ping(ctx); pingErr != nil {
            sug.Fatalw("db ping failed", "error", pingErr)
        }
        defer db.Close()
    } else {
        sug.Warn("DATABASE_URL is empty; running without DB connection")
    }

    lis, err := net.Listen("tcp", cfg.GRPCAddr)
    if err != nil {
        sug.Fatalw("listen failed", "error", err)
    }

    server := grpc.NewServer()

    // health service
    hs := health.NewServer()
    healthpb.RegisterHealthServer(server, hs)

    // register services generated from protobuf (no-op without build tag)
    registerGeneratedServices(server, db, cfg)

    go func() {
        sug.Infow("gRPC listening", "addr", cfg.GRPCAddr)
        if err := server.Serve(lis); err != nil {
            sug.Fatalw("gRPC server failed", "error", err)
        }
    }()

    // graceful shutdown
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    sug.Info("shutting down gRPC server...")
    done := make(chan struct{})
    go func() { server.GracefulStop(); close(done) }()
    select {
    case <-done:
        sug.Info("gRPC server stopped")
    case <-time.After(5 * time.Second):
        sug.Warn("force stopping gRPC server")
        server.Stop()
    }
}




