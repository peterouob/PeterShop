package event

import (
	"encoding/json"
	"testing"

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
