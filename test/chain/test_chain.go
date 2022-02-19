package chain

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/strangelove-ventures/ibc-test-framework/test/utils"
)

var (
	NETWORK_LABEL_KEY = "ibc-test"
)

type TestChain struct {
	T               *testing.T
	RootDataPath    string
	Pool            *dockertest.Pool
	Network         *docker.Network
	ChainID         string
	Nodes           []*TestNode
	NextNodeID      int
	ContainerConfig *ContainerConfig
}

func NewTestChain(
	t *testing.T, ctx context.Context,
	pool *dockertest.Pool,
	chainID string,
	numNodes int,
	containerConfig *ContainerConfig,
) *TestChain {
	c := &TestChain{
		T:            t,
		RootDataPath: "",
		Pool:         pool,
		Network:      nil,

		ChainID: chainID,
		Nodes:   []*TestNode{},

		NextNodeID:      0,
		ContainerConfig: containerConfig,
	}
	c.initHostEnv(ctx)
	for i := 0; i < numNodes; i += 1 {
		NewTestNode(c)
	}
	return c
}

func (c *TestChain) NetworkName() string {
	return fmt.Sprintf("%s-network", c.ChainID)
}

func (c *TestChain) Initialize(ctx context.Context) {
	var eg errgroup.Group
	for _, n := range c.Nodes {
		n := n
		eg.Go(func() error { n.Initialize(ctx); return nil })
	}
	require.NoError(c.T, eg.Wait())
}

func (c *TestChain) CreateGenesis(
	ctx context.Context,
	genValidators []*TestNode,
) {
	for _, v := range genValidators {
		require.Equalf(
			c.T, v.C, c, "validator %s is not part of chain %s", v.Name(), c.ChainID)
	}

	require.NotEqualf(
		c.T, len(genValidators), 0,
		"cannot create genesis file without at least one validator on chain %s",
		c.ChainID,
	)

	genV := genValidators[0]
	genV.CreateGenesisTx(ctx)

	eg := errgroup.Group{}
	for _, v := range genValidators[1:] {
		v := v
		eg.Go(func() error {
			v.CreateGenesisTx(ctx)

			key := v.GetKey(VALIDATOR_KEY_NAME)
			genV.Container.AddGenesisAccount(ctx, key.GetAddress().String())

			nodeID := v.TestNodeID()
			oldPath := path.Join(v.HostHomeDir(), "config", "gentx",
				fmt.Sprintf("gentx-%s.json", nodeID))
			newPath := path.Join(
				genV.HostHomeDir(), "config", "gentx",
				fmt.Sprintf("gentx-%s.json", nodeID))
			if err := os.Rename(oldPath, newPath); err != nil {
				return err
			}

			return nil
		})
	}
	require.NoErrorf(
		c.T, eg.Wait(), "failed to migrate genesis data for chain %s", c.ChainID)

	genV.Container.CollectGentxs(ctx)

	genesis, err := ioutil.ReadFile(genV.GenesisFilePath())
	require.NoErrorf(
		c.T, err, "failed to read genesis file %s", genV.GenesisFilePath())

	for _, node := range c.Nodes {
		require.NoError(
			c.T, ioutil.WriteFile(node.GenesisFilePath(), genesis, 0644),
			"failed to write genesis file %s", node.GenesisFilePath())
	}

	c.LogGenesisHashes()
}

func (c *TestChain) LogGenesisHashes() {
	for _, node := range c.Nodes {
		genesis, err := ioutil.ReadFile(node.GenesisFilePath())
		require.NoErrorf(
			c.T, err, "failed to read genesis file %s", node.GenesisFilePath())
		c.T.Logf("{%s} genesis hash %x", node.Name(), sha256.Sum256(genesis))
	}
}

func (c *TestChain) PeerString() string {
	bldr := new(strings.Builder)
	for _, node := range c.Nodes {
		peerString := node.PeerString()
		c.T.Logf("{%s} peering {%s}", node.Name(), peerString)
		_, err := bldr.WriteString(peerString + ",")
		require.NoError(c.T, err, "failed to build peer string")
	}
	return strings.TrimSuffix(bldr.String(), ",")
}

func (c *TestChain) Start(ctx context.Context) {
	eg := errgroup.Group{}
	for _, node := range c.Nodes {
		node := node
		c.T.Logf("{%s} => starting container...", node.Name())
		eg.Go(func() error { node.Start(ctx); return nil })
	}
	require.NoError(c.T, eg.Wait())
}

func (c *TestChain) WaitForHeight(ctx context.Context, height int64) {
	var eg errgroup.Group
	c.T.Logf("Waiting For Nodes To Reach Block Height %d...", height)
	for _, node := range c.Nodes {
		node := node
		eg.Go(func() error { node.WaitForHeight(ctx, height); return nil })
	}
	require.NoError(c.T, eg.Wait())
}

func (c *TestChain) initHostEnv(ctx context.Context) {
	c.T.Log("checking for docker artifacts from previous test")
	c.removeDockerArtifacts()

	// Create tmp directory for docker container mounts
	rootDataPath, err := utils.CreateTmpDir()
	require.NoErrorf(c.T, err, "failed to create tmp dir for chain %s", c.ChainID)
	c.RootDataPath = rootDataPath
	c.T.Log(rootDataPath)

	// Create docker network
	network, err := c.Pool.Client.CreateNetwork(docker.CreateNetworkOptions{
		Name:           c.NetworkName(),
		Options:        map[string]interface{}{},
		Labels:         map[string]string{NETWORK_LABEL_KEY: c.T.Name()},
		CheckDuplicate: true,
		Internal:       false,
		EnableIPv6:     false,
		Context:        ctx,
	})
	require.NoErrorf(c.T, err, "failed to create network for chain %s", c.ChainID)
	c.Network = network
	c.T.Cleanup(func() {
		c.removeDockerArtifacts()
	})
}

func (c *TestChain) removeDockerArtifacts() {
	containerFilter := map[string][]string{"network": {c.NetworkName()}}
	containers, err := c.Pool.Client.ListContainers(
		docker.ListContainersOptions{Filters: containerFilter})
	require.NoErrorf(c.T, err, "failed to list containers on chain %s", c.ChainID)

	eg := errgroup.Group{}
	for _, container := range containers {
		container := container
		c.T.Logf("removing container %s %v", container.ID, container.Names)
		eg.Go(func() error {
			return c.Pool.Client.RemoveContainer(docker.RemoveContainerOptions{
				ID:    container.ID,
				Force: true,
			})
		})
	}
	require.NoErrorf(
		c.T, eg.Wait(), "failed removing containers on chain %s", c.ChainID)

	networkFilter := map[string]map[string]bool{"name": {c.NetworkName(): true}}
	networks, err := c.Pool.Client.FilteredListNetworks(networkFilter)
	require.NoErrorf(c.T, err, "failed to list networks on chain %s", c.ChainID)
	for _, network := range networks {
		c.T.Logf("removing network %s", network.Name)
		require.NoErrorf(
			c.T, c.Pool.Client.RemoveNetwork(network.ID),
			"failed to remove network on chain %s", c.ChainID)
	}
}
