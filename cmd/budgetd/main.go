package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "os"
    "os/signal"
    "syscall"
    "time"

    "google.golang.org/grpc"
    health "google.golang.org/grpc/health"
    healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func getenv(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

func main() {
    grpcAddr := getenv("GRPC_ADDR", ":8080")

    lis, err := net.Listen("tcp", grpcAddr)
    if err != nil {
        log.Fatalf("failed to listen on %s: %v", grpcAddr, err)
    }

    server := grpc.NewServer()

    // Register health service
    hs := health.NewServer()
    healthpb.RegisterHealthServer(server, hs)

    // Lifecycle: serve in goroutine
    go func() {
        log.Printf("gRPC listening on %s", grpcAddr)
        if err := server.Serve(lis); err != nil {
            log.Fatalf("gRPC server failed: %v", err)
        }
    }()

    // Graceful shutdown on SIGINT/SIGTERM
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    <-sigCh
    log.Println("shutting down gRPC server...")

    // allow ongoing RPCs to finish
    done := make(chan struct{})
    go func() {
        server.GracefulStop()
        close(done)
    }()

    select {
    case <-done:
        log.Println("gRPC server stopped")
    case <-time.After(5 * time.Second):
        log.Println("force stopping gRPC server")
        server.Stop()
    }

    // Extra: ensure context cancel semantics for future extensions
    _ = context.Background()
    fmt.Println("bye")
}


