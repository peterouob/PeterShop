package rediskey

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActivityKeys(t *testing.T) {
	cases := []struct {
		name string
		fn   func(int64) string
		want string
	}{
		{name: "meta", fn: Meta, want: "seckill:{1}:meta"},
		{name: "slots", fn: Slots, want: "seckill:{1}:slots"},
		{name: "slots rebuild", fn: SlotsRebuild, want: "seckill:{1}:slots:rebuild"},
		{name: "users", fn: Users, want: "seckill:{1}:users"},
		{name: "users rebuild", fn: UsersRebuild, want: "seckill:{1}:users:rebuild"},
		{name: "outbox", fn: Outbox, want: "seckill:{1}:outbox"},
		{name: "released", fn: Released, want: "seckill:{1}:released"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.fn(1))
		})
	}
}

func TestActivityKeys_SnowflakeIDIsExactDecimal(t *testing.T) {
	const aid int64 = 1852301234567890123
	assert.Equal(t, "seckill:{1852301234567890123}:slots", Slots(aid))
}

func TestActivityKeys_SameHashTag(t *testing.T) {
	const aid int64 = 42
	keys := []string{Meta(aid), Slots(aid), SlotsRebuild(aid), Users(aid), UsersRebuild(aid), Outbox(aid), Released(aid)}

	for _, k := range keys {
		_, rest, ok := strings.Cut(k, "{")
		assert.True(t, ok, "%s needs {", k)
		tag, _, ok := strings.Cut(rest, "}")
		assert.True(t, ok, "%s needs }", k)
		assert.Equal(t, "42", tag, k)
	}
}

func TestLimitKeyIsPerUser(t *testing.T) {
	assert.Equal(t, "seckill:limit:u1", Limit("u1"))
}
