package mq

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithErrorHeader_DoesNotMutateOriginal(t *testing.T) {
	orig := Message{Key: "k", Payload: []byte("p"), Headers: map[string]string{"trace": "t1"}}

	got := WithErrorHeader(orig, "error", errors.New("bad json"))

	assert.Equal(t, map[string]string{"trace": "t1", "error": "bad json"}, got.Headers)
	assert.Equal(t, map[string]string{"trace": "t1"}, orig.Headers)
	assert.Equal(t, orig.Key, got.Key)
	assert.Equal(t, orig.Payload, got.Payload)
}

func TestWithErrorHeader_NilHeaders(t *testing.T) {
	got := WithErrorHeader(Message{Key: "k"}, "error", errors.New("bad json"))

	assert.Equal(t, map[string]string{"error": "bad json"}, got.Headers)
}

func TestWithErrorHeader_NilErrorStillExplains(t *testing.T) {
	got := WithErrorHeader(Message{}, "error", nil)

	assert.Equal(t, "unknown", got.Headers["error"])
}
