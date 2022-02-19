package test

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/ibc-test-framework/test/util"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	r, err := util.NewChainRunner(t, ctx, "ibc-test")
	require.NoError(t, err, "failed to create ChainRunner")

	for i := 0; i < 3; i += 1 {
		err := r.AddNode(&util.GaiaContainerConfig, true /* isValidator */)
		require.NoError(t, err, "failed to add node %d", i)
	}

	var eg errgroup.Group
	for _, node := range r.Nodes {
		node := node
		eg.Go(func() error { return node.Initialize(ctx) })
	}
	require.NoError(t, eg.Wait(), "Error initializing nodes")

	require.NoError(t, r.CreateGenesis(ctx))
}
