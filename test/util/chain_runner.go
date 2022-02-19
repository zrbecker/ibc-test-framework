package util

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"golang.org/x/sync/errgroup"
)

var (
	NETWORK_LABEL_KEY = "ibc-test"
	NETWORK_NAME      = "ibc-test-network"
)

type ChainRunner struct {
	T            *testing.T
	RootDataPath string
	Pool         *dockertest.Pool
	Network      *docker.Network

	ChainId string
	Nodes   []*Node

	nextNodeId int
}

func NewChainRunner(t *testing.T, ctx context.Context, chainId string) (*ChainRunner, error) {
	r := &ChainRunner{
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

func (r *ChainRunner) initHostEnv(ctx context.Context) error {
	pool, err := dockertest.NewPool("")
	if err != nil {
		return err
	}
	r.Pool = pool

	rootDataPath, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}
	r.RootDataPath = rootDataPath
	r.T.Cleanup(func() {
		_ = os.RemoveAll(r.RootDataPath)
	})
	r.T.Log(rootDataPath)

	// Remove docker network if it failed to cleanup after previous test run
	networks, err := r.Pool.Client.FilteredListNetworks(map[string]map[string]bool{"name": {NETWORK_NAME: true}})
	if err != nil {
		return err
	}
	for _, network := range networks {
		if err := r.Pool.Client.RemoveNetwork(network.ID); err != nil {
			return err
		}
	}

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
		_ = r.Pool.Client.RemoveNetwork(r.Network.ID)
	})

	return nil
}

func (r *ChainRunner) AddNode(containerConfig *ContainerConfig, isValidator bool) error {
	node, err := NewNode(r, r.nextNodeId, containerConfig, isValidator)
	if err != nil {
		return err
	}
	r.nextNodeId += 1
	r.Nodes = append(r.Nodes, node)
	return nil
}

func (r *ChainRunner) CreateGenesis(ctx context.Context) error {
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

func (r ChainRunner) LogGenesisHashes() error {
	for _, node := range r.Nodes {
		genesis, err := ioutil.ReadFile(node.GenesisFilePath())
		if err != nil {
			return err
		}
		r.T.Logf("{%s} genesis hash %x", node.Name(), sha256.Sum256(genesis))
	}
	return nil
}
