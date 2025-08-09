package logger

import (
	"go.uber.org/zap"
)

func New(env string) (*zap.Logger, error) {
	if env == "prod" { // production config: JSON, sampling, stacktraces on error
		return zap.NewProduction()
	}
	cfg := zap.NewDevelopmentConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	return cfg.Build()
}
