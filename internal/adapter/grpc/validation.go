package grpcadapter

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func invalidArg(msg string) error { return status.Error(codes.InvalidArgument, msg) }
