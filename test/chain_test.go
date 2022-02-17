package test

import (
	"context"
	"fmt"
	"testing"

	"github.com/strangelove-ventures/ibc-test-framework/test/util"
	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	r, err := util.NewChainRunner(t, ctx, "ibc-test")
	assert.NoError(t, err, "Error while initializing ChainRunner")

	numNodes := 3
	for i := 0; i < numNodes; i += 1 {
		err := r.AddNode(&util.GaiaContainerConfig)
		if err != nil {
			assert.NoError(t, err, fmt.Sprintf("Error adding node %d", i))
		}
	}
	r.AddNode(&util.GaiaContainerConfig)
	r.AddNode(&util.GaiaContainerConfig)

	assert.True(t, true)
}
