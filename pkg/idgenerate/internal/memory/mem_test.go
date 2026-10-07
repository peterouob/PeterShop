package memory

import (
	"testing"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord/coordtest"
)

func newCoord(t *testing.T) coord.Coordinator {
	return NewMemLease()
}

func TestMem_Grant(t *testing.T) {
	coordtest.RunGrantTest(t, newCoord)
}
