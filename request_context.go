package koba

import (
	"context"

	"github.com/rs/zerolog"
)

func SetupLoggerRequestContext(ctx context.Context, values map[string]string) context.Context {
	loggerContext := zerolog.Ctx(ctx).With()
	for name, value := range values {
		loggerContext = loggerContext.Str(name, value)
	}
	logger := loggerContext.Logger()
	ctx = logger.WithContext(ctx)
	return ctx
}
