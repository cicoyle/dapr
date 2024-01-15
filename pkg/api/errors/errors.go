package errors

import (
	"fmt"
	kiterrors "github.com/dapr/kit/errors"
	"google.golang.org/grpc/codes"
)

func NotFound(name string, componentType string, metadata map[string]string, grpcCode codes.Code, httpCode int, legacyTag string, reason string) error {
	message := fmt.Sprintf("%s %s not found", componentType, name)

	return kiterrors.NewBuilder(
		grpcCode,
		httpCode,
		message,
		legacyTag,
	).
		WithErrorInfo(reason, metadata).
		Build()
}

func NotConfigured(name string, componentType string, metadata map[string]string, grpcCode codes.Code, httpCode int, legacyTag string, reason string) error {
	message := fmt.Sprintf("%s %s not configured", componentType, name)

	return kiterrors.NewBuilder(
		grpcCode,
		httpCode,
		message,
		legacyTag,
	).
		WithErrorInfo(reason, metadata).
		Build()
}
