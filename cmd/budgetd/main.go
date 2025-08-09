package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/positron48/budget/internal/adapter/postgres"
	"github.com/positron48/budget/internal/pkg/config"
	"github.com/positron48/budget/internal/pkg/logger"

	budgetv1 "github.com/positron48/budget/gen/go/budget/v1"
	aauth "github.com/positron48/budget/internal/adapter/auth"
	grpcadapter "github.com/positron48/budget/internal/adapter/grpc"
	useauth "github.com/positron48/budget/internal/usecase/auth"
	"github.com/positron48/budget/internal/usecase/category"
	"github.com/positron48/budget/internal/usecase/tenant"

	// usecase imports will be wired when generated stubs are available
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

	// register services
	if db != nil {
		// middlewares
		_ = grpcadapter.AuthUnaryInterceptor // keep import until integrated into server options

		// Auth
		userRepo := postgres.NewUserRepo(db)
		rtRepo := postgres.NewRefreshTokenRepo(db)
		hasher := aauth.NewArgon2Hasher()
		issuer := aauth.NewJWTIssuer(cfg.JWTSignKey)
		authSvc := useauth.NewService(userRepo, rtRepo, hasher, issuer, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
		budgetv1.RegisterAuthServiceServer(server, grpcadapter.NewAuthServer(authSvc))

		// Tenant
		tenantRepo := postgres.NewTenantRepo(db)
		tenantSvc := tenant.NewService(tenantRepo)
		budgetv1.RegisterTenantServiceServer(server, grpcadapter.NewTenantServer(tenantSvc))

		// Category
		categoryRepo := postgres.NewCategoryRepo(db)
		categorySvc := category.NewService(categoryRepo)
		budgetv1.RegisterCategoryServiceServer(server, grpcadapter.NewCategoryServer(categorySvc))
	}

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
