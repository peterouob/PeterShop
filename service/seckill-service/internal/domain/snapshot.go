package domain

import "time"

type ActivitySnapshot struct {
	ActivityID int64
	StartAt    time.Time
	EndAt      time.Time
	Sealed     bool
	Slots      []int32
	Users      []string
}

var ErrActivityNotFound = &Error{"seckill activity not found"}
