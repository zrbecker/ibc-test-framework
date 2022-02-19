package test

import (
	"context"
	"testing"

	"github.com/ory/dockertest"
	"github.com/strangelove-ventures/ibc-test-framework/test/chain"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func CreateChain(
	t *testing.T,
	ctx context.Context,
	pool *dockertest.Pool,
	chainId string,
	numNodes int,
	numValidators int,
) *chain.TestChain {
	c, err := chain.NewTestChain(
		t, ctx, pool,
		chainId,
		numNodes,
		&chain.GaiaContainerConfig,
	)
	require.NoError(t, err, "failed to create test chain")

	require.NoError(t, c.Initialize(ctx))
	require.NoError(t, c.CreateGenesis(ctx, c.Nodes[:numValidators]))
	require.NoError(t, c.Start(ctx))

	return c
}

func TestRun(t *testing.T) {
	ctx := context.Background()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	var chain1, chain2 *chain.TestChain
	eg := errgroup.Group{}

	eg.Go(func() error { chain1 = CreateChain(t, ctx, pool, "ibc-test-1", 4, 3); return nil })
	eg.Go(func() error { chain2 = CreateChain(t, ctx, pool, "ibc-test-2", 4, 3); return nil })
	require.NoError(t, eg.Wait())

	eg.Go(func() error { return chain1.WaitForHeight(ctx, 10) })
	eg.Go(func() error { return chain2.WaitForHeight(ctx, 10) })
	require.NoError(t, eg.Wait())
}
