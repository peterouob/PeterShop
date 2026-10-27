package idgenerate

import "time"

type clock struct {
	wall func() time.Time
	base time.Time
}

func newClock(wall func() time.Time) clock {
	if wall == nil {
		wall = time.Now
	}
	return clock{wall: wall, base: time.Now()}
}

func (c clock) wallMs() int64 { return c.wall().UnixMilli() }

func (c clock) mono() time.Duration { return time.Since(c.base) }

type clockAnchor struct {
	wall time.Time
	mono time.Duration
}

func newClockAnchor(wall time.Time, mono time.Duration) clockAnchor {
	return clockAnchor{wall: wall.Round(0), mono: mono}
}

func (a clockAnchor) drift(wall time.Time, mono time.Duration) time.Duration {
	return wall.Sub(a.wall) - (mono - a.mono)
}
