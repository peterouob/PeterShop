package coord

import "time"

type LeaseGrantResp struct {
	ID  int64
	TTL time.Duration
}
