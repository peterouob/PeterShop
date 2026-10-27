package idgenerate

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComposeDecompose(t *testing.T) {
	testCase := []struct {
		name     string
		ts       int64
		workerID uint64
		seqID    uint64
	}{
		{name: "normal", ts: 1758412800000, workerID: 7, seqID: 5},
		{name: "overflow", ts: 1758412800000, workerID: maxWorkerID, seqID: sequenceMask},
	}

	for _, tc := range testCase {
		id := compose(tc.ts, tc.workerID, tc.seqID)
		gotTS, gotWorkerID, gotSeq := decompose(id)

		assert.Equal(t, tc.ts, gotTS)
		assert.Equal(t, tc.workerID, gotWorkerID)
		assert.Equal(t, tc.seqID, gotSeq)
	}
}

func TestSnowflakeNext(t *testing.T) {
	const (
		gorutineNum = 64
		perG        = 100_000
	)

	snow := generator{workerID: 1}
	epoch := time.Now()
	nowMs := func() int64 {
		return time.Since(epoch).Milliseconds()
	}

	t.Run("not duplicate", func(t *testing.T) {
		bucket := make([][]int64, gorutineNum)
		var wg sync.WaitGroup

		for g := range gorutineNum {
			wg.Go(func() {
				ids := make([]int64, 0, perG)
				for range perG {
					id, err := snow.next(nowMs())
					assert.NoError(t, err)
					ids = append(ids, id)
				}
				bucket[g] = ids
			})
		}

		wg.Wait()

		all := slices.Concat(bucket...)
		assert.Equal(t, gorutineNum*perG, len(all))
		slices.Sort(all)
		assert.Equal(t, len(all), len(slices.Compact(all)), "duplicate ids")
	})
}

func FuzzIDComposeDecompose(f *testing.F) {
	f.Add(int64(0), uint64(0), uint64(0))
	f.Add(int64(1), uint64(1), uint64(1))
	f.Add(int64(maxTimestamp), uint64(maxWorkerID), uint64(sequenceMask))
	f.Add(int64(1758412800000-1767225600000), uint64(7), uint64(5))

	f.Fuzz(func(t *testing.T, ts int64, workerID uint64, seqID uint64) {
		ts &= maxTimestamp
		workerID &= maxWorkerID
		seqID &= sequenceMask

		id := compose(ts, workerID, seqID)
		gotTS, gotWorkerID, gotSeq := decompose(id)
		assert.Equal(t, ts, gotTS)
		assert.Equal(t, workerID, gotWorkerID)
		assert.Equal(t, seqID, gotSeq)
	})
}
