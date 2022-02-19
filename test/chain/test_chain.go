package chain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/avast/retry-go"
	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"golang.org/x/sync/errgroup"

	"github.com/strangelove-ventures/ibc-test-framework/test/utils"
)

var (
	NETWORK_LABEL_KEY = "ibc-test"
	NETWORK_NAME      = "ibc-test-network"
)

type TestChain struct {
	T            *testing.T
	RootDataPath string
	Pool         *dockertest.Pool
	Network      *docker.Network

	ChainId string
	Nodes   []*TestNode
}

func NewTestChain(
	t *testing.T, ctx context.Context,
	pool *dockertest.Pool,
	chainId string,
	numNodes int,
	containerConfig *ContainerConfig,
) (*TestChain, error) {
	c := &TestChain{
		T:            t,
		RootDataPath: "",
		Pool:         pool,
		Network:      nil,

		ChainId: chainId,
		Nodes:   []*TestNode{},
	}
	if err := c.initHostEnv(ctx); err != nil {
		return nil, err
	}
	for i := 0; i < numNodes; i += 1 {
		if _, err := NewTestNode(c, i, containerConfig); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *TestChain) Initialize(ctx context.Context) error {
	var eg errgroup.Group
	for _, n := range c.Nodes {
		n := n
		eg.Go(func() error { return n.Initialize(ctx) })
	}
	return eg.Wait()
}

func (c *TestChain) CreateGenesis(ctx context.Context, genValidators []*TestNode) error {
	for _, v := range genValidators {
		if v.C != c {
			return fmt.Errorf("validator %s is not part of chain %s", v.Name(), c.ChainId)
		}
	}

	if len(genValidators) == 0 {
		return errors.New("cannot create genesis file without at least one validator")
	}

	eg := errgroup.Group{}
	genV := genValidators[0]
	if err := genV.CreateGenesisTx(ctx); err != nil {
		return err
	}

	for _, v := range genValidators[1:] {
		v := v
		eg.Go(func() error {
			if err := v.CreateGenesisTx(ctx); err != nil {
				return err
			}

			key, err := v.GetKey(VALIDATOR_KEY)
			if err != nil {
				return err
			}

			if err := genV.AddGenesisAccount(ctx, key.GetAddress().String()); err != nil {
				return err
			}

			nodeId, err := v.TestNodeID()
			if err != nil {
				return err
			}

			oldPath := path.Join(v.HostHomeDir(), "config", "gentx", fmt.Sprintf("gentx-%s.json", nodeId))
			newPath := path.Join(genV.HostHomeDir(), "config", "gentx", fmt.Sprintf("gentx-%s.json", nodeId))
			if err := os.Rename(oldPath, newPath); err != nil {
				return err
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	if err := genV.CollectGentxs(ctx); err != nil {
		return err
	}

	genesis, err := ioutil.ReadFile(genV.GenesisFilePath())
	if err != nil {
		return err
	}

	for _, node := range c.Nodes {
		if err := ioutil.WriteFile(node.GenesisFilePath(), genesis, 0644); err != nil {
			return err
		}
	}

	if err := c.LogGenesisHashes(); err != nil {
		return err
	}

	return nil
}

func (c *TestChain) LogGenesisHashes() error {
	for _, node := range c.Nodes {
		genesis, err := ioutil.ReadFile(node.GenesisFilePath())
		if err != nil {
			return err
		}
		c.T.Logf("{%s} genesis hash %x", node.Name(), sha256.Sum256(genesis))
	}
	return nil
}

func (c *TestChain) PeerString() (string, error) {
	bldr := new(strings.Builder)
	for _, node := range c.Nodes {
		peerString, err := node.PeerString()
		if err != nil {
			return "", err
		}
		c.T.Logf("{%s} peering {%s}", node.Name(), peerString)
		if _, err := bldr.WriteString(peerString + ","); err != nil {
			return "", err
		}
	}
	return strings.TrimSuffix(bldr.String(), ","), nil
}

func (c *TestChain) Start(ctx context.Context) error {
	eg := errgroup.Group{}
	for _, node := range c.Nodes {
		node := node
		c.T.Logf("{%s} => starting container...", node.Name())
		eg.Go(func() error {
			if err := node.SetValidatorConfig(); err != nil {
				return err
			}
			if err := node.Start(ctx); err != nil {
				return err
			}
			if err := node.SetupAndVerify(ctx); err != nil {
				return err
			}
			return nil
		})
	}
	return eg.Wait()
}

func (c *TestChain) WaitForHeight(ctx context.Context, height int64) error {
	var eg errgroup.Group
	c.T.Logf("Waiting For Nodes To Reach Block Height %d...", height)
	for _, node := range c.Nodes {
		node := node
		eg.Go(func() error {
			return retry.Do(func() error {
				stat, err := node.Client.Status(ctx)
				if err != nil {
					return err
				}

				if stat.SyncInfo.CatchingUp || stat.SyncInfo.LatestBlockHeight < height {
					return fmt.Errorf("node still under block %d: %d", height, stat.SyncInfo.LatestBlockHeight)
				}
				c.T.Logf("{%s} => reached block %d\n", node.Name(), height)
				return nil
				// TODO: setup backup delay here
			}, retry.DelayType(retry.BackOffDelay), retry.Attempts(15))
		})
	}
	return eg.Wait()
}

func (c *TestChain) initHostEnv(ctx context.Context) error {
	if err := c.removeDockerArtifactsFromPreviousTest(); err != nil {
		return err
	}

	// Create tmp directory for docker container mounts
	rootDataPath, err := utils.CreateTmpDir()
	if err != nil {
		return err
	}
	c.RootDataPath = rootDataPath
	c.T.Log(rootDataPath)

	// Create docker network
	network, err := c.Pool.Client.CreateNetwork(docker.CreateNetworkOptions{
		Name:           NETWORK_NAME,
		Options:        map[string]interface{}{},
		Labels:         map[string]string{NETWORK_LABEL_KEY: c.T.Name()},
		CheckDuplicate: true,
		Internal:       false,
		EnableIPv6:     false,
		Context:        ctx,
	})
	if err != nil {
		return err
	}
	c.Network = network
	c.T.Cleanup(func() {
		err = c.Pool.Client.RemoveNetwork(c.Network.ID)
		if err != nil {
			c.T.Logf("failed to remove docker network on test cleanup %+v", err)
		}
	})

	return nil
}

func (c *TestChain) removeDockerArtifactsFromPreviousTest() error {
	containerFilter := map[string][]string{"network": {NETWORK_NAME}}
	containers, err := c.Pool.Client.ListContainers(docker.ListContainersOptions{Filters: containerFilter})
	if err != nil {
		return err
	}
	for _, container := range containers {
		c.T.Logf("removing container %s %v from previous test", container.ID, container.Names)
		opts := docker.RemoveContainerOptions{ID: container.ID, Force: true}
		if err := c.Pool.Client.RemoveContainer(opts); err != nil {
			return err
		}
	}

	networkFilter := map[string]map[string]bool{"name": {NETWORK_NAME: true}}
	networks, err := c.Pool.Client.FilteredListNetworks(networkFilter)
	if err != nil {
		return err
	}
	for _, network := range networks {
		c.T.Logf("removing network %s from previous test", network.Name)
		if err := c.Pool.Client.RemoveNetwork(network.ID); err != nil {
			return err
		}
	}
	return nil
}
