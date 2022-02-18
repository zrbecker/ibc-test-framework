package test

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/ibc-test-framework/test/util"
	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	r, err := util.NewChainRunner(t, ctx, "ibc-test")
	if !assert.NoError(t, err, "Error while initializing ChainRunner") {
		return
	}

	numValidators := 3
	for i := 0; i < numValidators; i += 1 {
		err := r.AddNode(&util.GaiaContainerConfig, true)
		if !assert.NoError(t, err, "Error adding node %d", i) {
			return
		}
	}

	for _, node := range r.Nodes {
		err := node.Initialize(ctx)
		if !assert.NoError(t, err, "Error adding node %d", node.Id) {
			return
		}
	}

	assert.True(t, true)
}
