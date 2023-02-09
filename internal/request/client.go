package request

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Client interface {
	Create(ctx context.Context, object client.Object, opts ...client.CreateOption) error
}
