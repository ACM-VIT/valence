package memqueue

import (
	"testing"

	"github.com/ACM-VIT/valence/pkg/queue/queuetest"
)

func TestConformance(t *testing.T) {
	queuetest.Run(t, New())
}
