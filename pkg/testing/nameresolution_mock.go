package testing

import (
	"context"

	mock "github.com/stretchr/testify/mock"

	nr "github.com/dapr/components-contrib/nameresolution"
)

// MockResolver is a mock nameresolution component object.
type MockResolver struct {
	mock.Mock
}

// Init is a mock initialization method.
func (m *MockResolver) Init(_ context.Context, metadata nr.Metadata) error {
	args := m.Called(metadata)
	return args.Error(0)
}

// ResolveID is a mock resolve method.
func (m *MockResolver) ResolveID(_ context.Context, req nr.ResolveRequest) (string, error) {
	args := m.Called(req)
	return args.String(0), args.Error(1)
}

func (m *MockResolver) Close() error {
	return nil
}
