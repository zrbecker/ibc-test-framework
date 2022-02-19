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
	Nodes   []*Node

	nextNodeId int
}

func NewTestChain(t *testing.T, ctx context.Context, chainId string) (*TestChain, error) {
	r := &TestChain{
		T:            t,
		RootDataPath: "",
		Pool:         nil,
		Network:      nil,

		ChainId: chainId,
		Nodes:   []*Node{},

		nextNodeId: 0,
	}
	if err := r.initHostEnv(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *TestChain) initHostEnv(ctx context.Context) error {
	pool, err := dockertest.NewPool("")
	if err != nil {
		return err
	}
	r.Pool = pool

	if err := r.removeDockerArtifactsFromPreviousTest(); err != nil {
		return err
	}

	rootDataPath, err := utils.CreateTmpDir()
	if err != nil {
		return err
	}
	r.RootDataPath = rootDataPath
	r.T.Log(rootDataPath)

	// Create docker network
	network, err := r.Pool.Client.CreateNetwork(docker.CreateNetworkOptions{
		Name:           NETWORK_NAME,
		Options:        map[string]interface{}{},
		Labels:         map[string]string{NETWORK_LABEL_KEY: r.T.Name()},
		CheckDuplicate: true,
		Internal:       false,
		EnableIPv6:     false,
		Context:        ctx,
	})
	if err != nil {
		return err
	}
	r.Network = network
	r.T.Cleanup(func() {
		err = r.Pool.Client.RemoveNetwork(r.Network.ID)
		if err != nil {
			r.T.Logf("failed to remove docker network on test cleanup %+v", err)
		}
	})

	return nil
}

func (r *TestChain) AddNode(containerConfig *ContainerConfig, isValidator bool) error {
	node, err := NewNode(r, r.nextNodeId, containerConfig, isValidator)
	if err != nil {
		return err
	}
	r.nextNodeId += 1
	r.Nodes = append(r.Nodes, node)
	return nil
}

func (r *TestChain) CreateGenesis(ctx context.Context) error {
	validators := []*Node{}
	for _, node := range r.Nodes {
		if node.IsValidator {
			validators = append(validators, node)
		}
	}

	if len(validators) == 0 {
		return errors.New("cannot create genesis file without at least one validator")
	}

	eg := errgroup.Group{}
	genValidator := validators[0]
	if err := genValidator.CreateGenesisTx(ctx); err != nil {
		return err
	}

	for _, validator := range validators[1:] {
		validator := validator
		eg.Go(func() error {
			if err := validator.CreateGenesisTx(ctx); err != nil {
				return err
			}

			key, err := validator.GetKey(VALIDATOR_KEY)
			if err != nil {
				return err
			}

			if err := genValidator.AddGenesisAccount(ctx, key.GetAddress().String()); err != nil {
				return err
			}

			nodeId, err := validator.NodeID()
			if err != nil {
				return err
			}

			oldPath := path.Join(validator.HostHomeDir(), "config", "gentx", fmt.Sprintf("gentx-%s.json", nodeId))
			newPath := path.Join(genValidator.HostHomeDir(), "config", "gentx", fmt.Sprintf("gentx-%s.json", nodeId))
			if err := os.Rename(oldPath, newPath); err != nil {
				return err
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	if err := genValidator.CollectGentxs(ctx); err != nil {
		return err
	}

	genesis, err := ioutil.ReadFile(genValidator.GenesisFilePath())
	if err != nil {
		return err
	}

	for _, node := range r.Nodes {
		if err := ioutil.WriteFile(node.GenesisFilePath(), genesis, 0644); err != nil {
			return err
		}
	}

	if err := r.LogGenesisHashes(); err != nil {
		return err
	}

	return nil
}

func (r *TestChain) LogGenesisHashes() error {
	for _, node := range r.Nodes {
		genesis, err := ioutil.ReadFile(node.GenesisFilePath())
		if err != nil {
			return err
		}
		r.T.Logf("{%s} genesis hash %x", node.Name(), sha256.Sum256(genesis))
	}
	return nil
}

func (r *TestChain) PeerString() (string, error) {
	bldr := new(strings.Builder)
	for _, node := range r.Nodes {
		peerString, err := node.PeerString()
		if err != nil {
			return "", err
		}
		r.T.Logf("{%s} peering {%s}", node.Name(), peerString)
		if _, err := bldr.WriteString(peerString + ","); err != nil {
			return "", err
		}
	}
	return strings.TrimSuffix(bldr.String(), ","), nil
}

func (r *TestChain) StartNodes(ctx context.Context) error {
	eg := errgroup.Group{}
	for _, node := range r.Nodes {
		node := node
		r.T.Logf("{%s} => starting container...", node.Name())
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

func (r *TestChain) WaitForHeight(ctx context.Context, height int64) error {
	var eg errgroup.Group
	r.T.Logf("Waiting For Nodes To Reach Block Height %d...", height)
	for _, node := range r.Nodes {
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
				r.T.Logf("{%s} => reached block %d\n", node.Name(), height)
				return nil
				// TODO: setup backup delay here
			}, retry.DelayType(retry.BackOffDelay), retry.Attempts(15))
		})
	}
	return eg.Wait()
}

func (r *TestChain) removeDockerArtifactsFromPreviousTest() error {
	containerFilter := map[string][]string{"network": {NETWORK_NAME}}
	containers, err := r.Pool.Client.ListContainers(docker.ListContainersOptions{Filters: containerFilter})
	if err != nil {
		return err
	}
	for _, container := range containers {
		r.T.Logf("removing container %s %v from previous test", container.ID, container.Names)
		opts := docker.RemoveContainerOptions{ID: container.ID, Force: true}
		if err := r.Pool.Client.RemoveContainer(opts); err != nil {
			return err
		}
	}

	networkFilter := map[string]map[string]bool{"name": {NETWORK_NAME: true}}
	networks, err := r.Pool.Client.FilteredListNetworks(networkFilter)
	if err != nil {
		return err
	}
	for _, network := range networks {
		r.T.Logf("removing network %s from previous test", network.Name)
		if err := r.Pool.Client.RemoveNetwork(network.ID); err != nil {
			return err
		}
	}
	return nil
}
