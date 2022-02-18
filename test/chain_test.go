package test

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/ibc-test-framework/test/util"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	r, err := util.NewChainRunner(t, ctx, "ibc-test")
	require.NoError(t, err, "Error while initializing ChainRunner")

	for i := 0; i < 3; i += 1 {
		err := r.AddNode(&util.GaiaContainerConfig, true /* isValidator */)
		require.NoError(t, err, "Error adding node %d", i)
	}

	for _, node := range r.Nodes {
		err := node.Initialize(ctx)
		require.NoError(t, err, "Error adding node %d", node.Id)
	}
}
