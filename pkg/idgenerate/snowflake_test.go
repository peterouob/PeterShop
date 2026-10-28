package idgenerate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestSnowflake() *Snowflake {
	return &Snowflake{clock: newClock(func() time.Time { return time.UnixMilli(epochMs + 1000) })}
}

func newTestSession(untilMs int64) *session {
	s := newSession(7, 1, 0, clockAnchor{})
	s.lease.extend(time.Hour)
	s.reserve.advance(untilMs)
	return s
}

func TestSnowflakeNext_NoSession(t *testing.T) {
	sf := newTestSnowflake()

	_, err := sf.Next()
	assert.ErrorIs(t, err, ErrNotReady)
}

func TestSnowflakeNext_OK(t *testing.T) {
	sf := newTestSnowflake()
	sf.cur.Store(newTestSession(2000))

	id, err := sf.Next()
	assert.NoError(t, err)

	ts, workerID, _ := decompose(id)
	assert.Equal(t, int64(1000), ts)
	assert.Equal(t, uint64(7), workerID)
}

func TestSnowflakeNext_LeaseExpired(t *testing.T) {
	sf := newTestSnowflake()
	s := newTestSession(2000)
	s.lease.revoke()
	sf.cur.Store(s)

	_, err := sf.Next()
	assert.ErrorIs(t, err, ErrLeaseExpired)
}

func TestSnowflakeNext_ReservationExhausted(t *testing.T) {
	sf := newTestSnowflake()
	sf.cur.Store(newTestSession(999))

	_, err := sf.Next()
	assert.ErrorIs(t, err, ErrReservationExhausted)
}

func TestSnowflakeNext_LeaseExpiresOverTime(t *testing.T) {
	sf := newTestSnowflake()
	s := newSession(7, 1, 0, clockAnchor{})
	s.reserve.advance(2000)
	s.lease.extend(sf.clock.mono() + time.Millisecond)
	sf.cur.Store(s)

	time.Sleep(5 * time.Millisecond)

	_, err := sf.Next()
	assert.ErrorIs(t, err, ErrLeaseExpired)
}
