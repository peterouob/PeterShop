package kafka

import (
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/peterouob/seckill_service/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validKafka() config.Kafka {
	return config.Kafka{
		Brokers:      []string{"kafka:9092"},
		ClientID:     "seckill-service",
		RequiredAcks: -1,
		MaxRetries:   5,
		RetryBackoff: 500 * time.Millisecond,
	}
}

func TestProducerConfig_Valid(t *testing.T) {
	t.Run("success config", func(t *testing.T) {
		_, err := producerConfig(validKafka())
		require.NoError(t, err)
	})

	t.Run("failed to config", func(t *testing.T) {
		k := validKafka()
		k.MaxRetries = 0
		_, err := producerConfig(k)

		var configErr sarama.ConfigurationError

		require.ErrorAs(t, err, &configErr)
	})
}

func TestProducerConfig_IdempotentAndSync(t *testing.T) {
	c, err := producerConfig(validKafka())
	require.NoError(t, err)

	assert.True(t, c.Producer.Idempotent)
	assert.Equal(t, sarama.WaitForAll, c.Producer.RequiredAcks)
	assert.Equal(t, 1, c.Net.MaxOpenRequests, "sarama's idempotent producer allows one in-flight request per connection")
	assert.True(t, c.Producer.Return.Successes, "SyncProducer needs Return.Successes")
}

func TestProducerConfig_FieldsComeFromConfig(t *testing.T) {
	k := validKafka()
	k.MaxRetries = 7
	k.RetryBackoff = 250 * time.Millisecond

	c, err := producerConfig(k)
	require.NoError(t, err)
	assert.Equal(t, "seckill-service", c.ClientID)
	assert.Equal(t, 7, c.Producer.Retry.Max)
	assert.Equal(t, 250*time.Millisecond, c.Producer.Retry.Backoff)
}

func TestProducerConfig_Rejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*config.Kafka)
	}{
		{"acks=1 loses messages when the leader dies", func(k *config.Kafka) { k.RequiredAcks = 1 }},
		{"acks=0 never waits", func(k *config.Kafka) { k.RequiredAcks = 0 }},
		{"zero retries is invalid for an idempotent producer", func(k *config.Kafka) { k.MaxRetries = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := validKafka()
			tc.mutate(&k)
			_, err := producerConfig(k)
			require.Error(t, err, "must fail at startup, not on the first publish")
		})
	}
}
