package test

import (
	"context"
	"testing"

	"github.com/ory/dockertest"
	"github.com/strangelove-ventures/ibc-test-framework/test/chain"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	c, err := chain.NewTestChain(t, ctx, pool, "ibc-test")
	require.NoError(t, err, "failed to create ChainRunner")

	for i := 0; i < 3; i += 1 {
		err := c.AddNode(&chain.GaiaContainerConfig, true /* isValidator */)
		require.NoError(t, err, "failed to add node %d", i)
	}

	var eg errgroup.Group
	for _, n := range c.Nodes {
		n := n
		eg.Go(func() error { return n.Initialize(ctx) })
	}
	require.NoError(t, eg.Wait(), "Error initializing nodes")

	require.NoError(t, c.CreateGenesis(ctx))

	require.NoError(t, c.Start(ctx))

	require.NoError(t, c.WaitForHeight(ctx, 10))
}
