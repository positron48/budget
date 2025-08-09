package logger

import (
	"context"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(env string) (*zap.Logger, error) {
	if env == "prod" { // production config: JSON, sampling, stacktraces on error
		cfg := zap.NewProductionConfig()
		cfg.EncoderConfig.TimeKey = "ts"
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339Nano)
		return cfg.Build()
	}
	cfg := zap.NewDevelopmentConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339Nano)
	return cfg.Build()
}

// StartMetricsServer starts a simple HTTP server to expose Prometheus metrics at /metrics.
// If addr is empty, it returns a no-op shutdown function.
func StartMetricsServer(lg *zap.SugaredLogger, addr string, handler http.Handler) func(context.Context) {
	if addr == "" {
		return func(ctx context.Context) {}
	}
	if handler == nil {
		handler = http.DefaultServeMux
	}
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		lg.Infow("metrics server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lg.Errorw("metrics server failed", "error", err)
		}
	}()
	return func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// IsCI returns true when running in CI environment.
func IsCI() bool { return os.Getenv("CI") != "" }
