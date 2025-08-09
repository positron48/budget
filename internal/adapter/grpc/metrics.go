package grpcadapter

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	grpcRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "budget",
			Subsystem: "grpc",
			Name:      "requests_total",
			Help:      "Total number of gRPC requests.",
		},
		[]string{"method", "code"},
	)
	grpcRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "budget",
			Subsystem: "grpc",
			Name:      "request_duration_seconds",
			Help:      "Duration of gRPC requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "code"},
	)
)

func init() {
	prometheus.MustRegister(grpcRequestsTotal, grpcRequestDuration)
}

// MetricsUnaryInterceptor records request count and latency per method and status code.
func MetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err).String()
		labels := prometheus.Labels{"method": info.FullMethod, "code": code}
		grpcRequestsTotal.With(labels).Inc()
		grpcRequestDuration.With(labels).Observe(time.Since(started).Seconds())
		return resp, err
	}
}
