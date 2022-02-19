package test

import (
	"context"
	"testing"

	"github.com/ory/dockertest"
	"github.com/strangelove-ventures/ibc-test-framework/test/chain"
	"github.com/strangelove-ventures/ibc-test-framework/test/relayer"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func CreateChain(
	t *testing.T,
	ctx context.Context,
	pool *dockertest.Pool,
	chainID string,
	numNodes int,
	numValidators int,
) *chain.TestChain {
	c := chain.NewTestChain(
		t, ctx, pool,
		chainID,
		numNodes,
		&chain.GaiaContainerConfig,
	)

	c.Initialize(ctx)
	c.CreateGenesis(ctx, c.Nodes[:numValidators])
	c.Start(ctx)

	return c
}

func CreatePostGenNode(
	t *testing.T,
	ctx context.Context,
	c *chain.TestChain,
) *chain.TestNode {
	existingNode := c.Nodes[0]

	node := chain.NewTestNode(c)

	node.Initialize(ctx)
	node.CopyGenesisFileFromNode(existingNode)
	node.Start(ctx)

	return node
}

func TestRun(t *testing.T) {
	ctx := context.Background()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	var chain1, chain2 *chain.TestChain
	eg := errgroup.Group{}

	chain1ID := "ibc-test-1"
	chain2ID := "ibc-test-2"
	eg.Go(func() error {
		chain1 = CreateChain(
			t, ctx, pool,
			chain1ID,
			3 /* nodes */, 3, /* validators */
		)
		return nil
	})
	eg.Go(func() error {
		chain2 = CreateChain(
			t, ctx, pool,
			chain2ID,
			3 /* nodes */, 3, /* validators */
		)
		return nil
	})
	require.NoError(t, eg.Wait())

	eg.Go(func() error { chain1.WaitForHeight(ctx, 5); return nil })
	eg.Go(func() error { chain2.WaitForHeight(ctx, 5); return nil })
	require.NoError(t, eg.Wait())

	// Do Relayer Stuff

	chain1Node := CreatePostGenNode(t, ctx, chain1)
	chain2Node := CreatePostGenNode(t, ctx, chain2)
	t.Log("new client nodes are waiting for height 20")
	eg.Go(func() error { chain1Node.WaitForHeight(ctx, 10); return nil })
	eg.Go(func() error { chain2Node.WaitForHeight(ctx, 10); return nil })
	require.NoError(t, eg.Wait())

	rly := relayer.NewTestRelayer(t, pool, "rly", "0.0.1", "rly")

	rly.Initialize(ctx, chain1Node, chain2Node)
}
