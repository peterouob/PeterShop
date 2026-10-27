package memory

import (
	"testing"

	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord"
	"github.com/peterouob/seckill_service/pkg/idgenerate/internal/coord/coordtest"
)

func TestContract(t *testing.T) {
	coordtest.RunGrantTest(t, func(t *testing.T) coord.Coordinator { return New() })
	coordtest.Run(t, func(t *testing.T) coord.Coordinator { return New() })
}
