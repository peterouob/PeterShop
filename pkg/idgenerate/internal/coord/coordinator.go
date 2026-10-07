package coord

import (
	"context"
	"time"
)

type Coordinator interface {
	Grant(ctx context.Context, ttl time.Duration) (*LeaseGrantResp, error)
}
