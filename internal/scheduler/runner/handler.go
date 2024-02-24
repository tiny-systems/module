package runner

import (
	"context"
)

type Handler func(ctx context.Context, msg *Msg) error
