package logger

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestStartMetricsServer_NoAddr(t *testing.T) {
	lg := zap.NewNop().Sugar()
	stop := StartMetricsServer(lg, "", nil)
	if stop == nil {
		t.Fatal("expected noop stop function")
	}
	stop(context.Background())
}

func TestStartMetricsServer_WithAddr(t *testing.T) {
	lg := zap.NewNop().Sugar()
	stop := StartMetricsServer(lg, "127.0.0.1:0", nil)
	if stop == nil {
		t.Fatal("expected stop function")
	}
	stop(context.Background())
}

func TestSetupTracing_NoEndpoint(t *testing.T) {
	stop, err := SetupTracing(context.Background(), "", true, "svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stop == nil {
		t.Fatal("expected shutdown func")
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("shutdown err: %v", err)
	}
}
