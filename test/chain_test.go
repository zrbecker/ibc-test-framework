package test

import (
	"context"
	"testing"

	"github.com/ory/dockertest"
	"github.com/strangelove-ventures/ibc-test-framework/test/chain"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	numNodes := 3
	c, err := chain.NewTestChain(
		t, ctx, pool,
		"ibc-test", /* chainID */
		numNodes,
		&chain.GaiaContainerConfig,
	)
	require.NoError(t, err, "failed to create test chain")

	require.NoError(t, c.Initialize(ctx))

	require.NoError(t, c.CreateGenesis(ctx, c.Nodes /* genValidators */))

	require.NoError(t, c.Start(ctx))

	require.NoError(t, c.WaitForHeight(ctx, 10))
}
