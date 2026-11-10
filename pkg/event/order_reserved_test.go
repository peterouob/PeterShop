package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/peterouob/seckill_service/pkg/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEvent() OrderReserved {
	return OrderReserved{
		OrderNo:     1852301234567890123,
		UserID:      "0190f3a2-7c4e-7b1a-9d2e-5f6a7b8c9d0e",
		ProductID:   "p1",
		ActivityID:  1852301234567000000,
		Amount:      100,
		CreatedAtMs: 1793000000000,
	}
}

const validJSON = `{"order_no":1852301234567890123,"user_id":"0190f3a2-7c4e-7b1a-9d2e-5f6a7b8c9d0e",` +
	`"product_id":"p1","activity_id":1852301234567000000,"amount":100,"created_at_ms":1793000000000}`

func TestDecode_WireFormat(t *testing.T) {
	ev, err := Decode([]byte(validJSON))
	require.NoError(t, err)
	assert.Equal(t, validEvent(), ev, "json keys are the contract between relay and both consumers")
}

func TestDecode_RoundTrip(t *testing.T) {
	b, err := json.Marshal(validEvent())
	require.NoError(t, err)
	ev, err := Decode(b)
	require.NoError(t, err)
	assert.Equal(t, validEvent(), ev)
}

func TestDecode_UnknownFieldIsNotPoison(t *testing.T) {
	b := []byte(validJSON[:len(validJSON)-1] + `,"coupon_id":7}`)
	_, err := Decode(b)
	require.NoError(t, err, "a newer producer adding a field must not send every message of an older consumer to the DLQ")
}

func TestDecode_Poison(t *testing.T) {
	mutate := func(f func(m map[string]any)) []byte {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(validJSON), &m))
		f(m)
		b, err := json.Marshal(m)
		require.NoError(t, err)
		return b
	}
	cases := []struct {
		name string
		in   []byte
	}{
		{"not json", []byte("not json")},
		{"missing order_no", mutate(func(m map[string]any) { delete(m, "order_no") })},
		{"order_no=0", mutate(func(m map[string]any) { m["order_no"] = 0 })},
		{"order_no as string", mutate(func(m map[string]any) { m["order_no"] = "1852301234567890123" })},
		{"missing activity_id", mutate(func(m map[string]any) { delete(m, "activity_id") })},
		{"product id is white space", mutate(func(m map[string]any) { m["product_id"] = " " })},
		{"missing user_id", mutate(func(m map[string]any) { delete(m, "user_id") })},
		{"missing product_id", mutate(func(m map[string]any) { delete(m, "product_id") })},
		{"zero amount", mutate(func(m map[string]any) { m["amount"] = 0 })},
		{"missing created_at_ms", mutate(func(m map[string]any) { delete(m, "created_at_ms") })},
		{"user_id longer than varchar(64)", mutate(func(m map[string]any) { m["user_id"] = strings.Repeat("u", 65) })},
		{"product_id longer than varchar(64)", mutate(func(m map[string]any) { m["product_id"] = strings.Repeat("p", 65) })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.in)
			require.ErrorIs(t, err, ErrPoison)
		})
	}
}

func TestDecodeBatch_SplitsPoison(t *testing.T) {
	good := mq.Message{Key: "1", Payload: []byte(validJSON)}
	bad := mq.Message{Key: "2", Payload: []byte("not json")}

	ok, poison := DecodeBatch([]mq.Message{good, bad, good})

	require.Len(t, ok, 2)
	assert.Equal(t, validEvent(), ok[0])
	require.Len(t, poison, 1)
	assert.Equal(t, bad, poison[0].Msg, "the original message goes to the DLQ untouched")
	assert.ErrorIs(t, poison[0].Err, ErrPoison)
}

func TestExpireAtMs_UsesReservationTime(t *testing.T) {
	e := OrderReserved{CreatedAtMs: 1_794_000_000_000}
	assert.Equal(t, int64(1_794_000_900_000), e.ExpireAtMs(15*time.Minute))
}

func TestDecode_IDLengthCountsCharacters(t *testing.T) {
	cases := []struct {
		name string
		unit string
	}{
		{"CJK BMP (3 bytes/rune)", "測品"},
		{"CJK rare BMP char (3 bytes/rune)", "堃"},
		{"CJK supplementary plane (4 bytes/rune)", "𪚥"},
		{"emoji (4 bytes/rune)", "😀"},
		{"ideographic description sequence (3 runes, 3 glyphs)", "⿱⺈目"},
		{"bopomofo with spacing tone mark (3 runes, 3 glyphs)", "ㄋㄧˇ"},
		{"combining acute accent (2 runes, 1 glyph)", "e\u0301"},
		{"combining caron on bopomofo (2 runes, 1 glyph)", "ㄧ\u030c"},
		{"ZWJ emoji sequence (5 runes, 1 glyph)", "👨\u200d👩\u200d👧"},
	}

	fields := []struct {
		name string
		set  func(*OrderReserved, string)
	}{
		{"user_id", func(e *OrderReserved, s string) { e.UserID = s }},
		{"product_id", func(e *OrderReserved, s string) { e.ProductID = s }},
	}

	for _, tc := range cases {
		for _, f := range fields {
			t.Run(tc.name+"/"+f.name, func(t *testing.T) {
				t.Run("at limit is accepted", func(t *testing.T) {
					id := repeatRunes(tc.unit, maxIDLen)
					require.Equal(t, maxIDLen, utf8.RuneCountInString(id))
					require.Greater(t, len(id), maxIDLen)

					want, got, err := decodeWithID(t, f.set, id)
					require.NoError(t, err)
					require.Equal(t, want, got)
				})

				t.Run("over limit is rejected", func(t *testing.T) {
					id := repeatRunes(tc.unit, maxIDLen+1)
					require.Equal(t, maxIDLen+1, utf8.RuneCountInString(id))

					_, _, err := decodeWithID(t, f.set, id)
					require.ErrorIs(t, err, ErrPoison)
					require.ErrorContains(t, err, f.name)
				})
			})
		}
	}
}

func repeatRunes(unit string, n int) string {
	src := []rune(unit)
	out := make([]rune, n)
	for i := range out {
		out[i] = src[i%len(src)]
	}
	return string(out)
}

func decodeWithID(
	t *testing.T,
	set func(*OrderReserved, string),
	id string,
) (want, got OrderReserved, err error) {
	t.Helper()

	want = validEvent()
	set(&want, id)

	b, err := json.Marshal(want)
	require.NoError(t, err)

	got, err = Decode(b)
	return want, got, err
}
