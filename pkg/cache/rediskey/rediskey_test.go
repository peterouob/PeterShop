package rediskey

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeys(t *testing.T) {
	testCase := []struct {
		name string
		fn   func(string) string
		in   string
		want string
	}{
		{name: "meta", fn: Meta, in: "p1", want: "seckill:{p1}:meta"},
		{name: "stock", fn: Stock, in: "p1", want: "seckill:{p1}:stock"},
		{name: "users", fn: Users, in: "p1", want: "seckill:{p1}:users"},
		{name: "outbox", fn: Outbox, in: "p1", want: "seckill:{p1}:outbox"},
		{name: "released", fn: Released, in: "p1", want: "seckill:{p1}:released"},
		{name: "limit", fn: Limit, in: "u1", want: "seckill:limit:u1"},
	}

	for _, tc := range testCase {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.fn(tc.in))
		})
	}
}

func TestKeys_SameHashTag(t *testing.T) {
	const pid = "p1"
	keys := []string{Meta(pid), Stock(pid), Users(pid), Outbox(pid), Released(pid)}

	for _, k := range keys {
		_, rest, ok := strings.Cut(k, "{")
		assert.True(t, ok, "%s need {", k)
		tag, _, ok := strings.Cut(rest, "}")
		assert.True(t, ok, "%s need }", k)
		assert.Equal(t, pid, tag, k)
	}
}
