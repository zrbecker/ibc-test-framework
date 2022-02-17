package util

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
)

var (
	NETWORK_LABEL_KEY = "ibc-test"
	NETWORK_NAME      = "ibc-test-network"
)

type ChainRunner struct {
	t            *testing.T
	rootDataPath string
	pool         *dockertest.Pool
	network      *docker.Network

	chainId string
	nodes   []*Node

	nextNodeId int
}

func NewChainRunner(t *testing.T, ctx context.Context, chainId string) (*ChainRunner, error) {
	r := &ChainRunner{
		t:            t,
		rootDataPath: "",
		pool:         nil,
		network:      nil,

		chainId:    chainId,
		nodes:      []*Node{},
		nextNodeId: 0,
	}
	if err := r.initHostEnv(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ChainRunner) AddNode(containerConfig *ContainerConfig) error {
	node, err := NewNode(r, r.nextNodeId, containerConfig)
	if err != nil {
		return err
	}
	r.nextNodeId += 1
	r.nodes = append(r.nodes, node)
	return nil
}

// InitHostEnv creates docker host dependencies needed to run chain
func (r *ChainRunner) initHostEnv(ctx context.Context) error {
	pool, err := dockertest.NewPool("")
	if err != nil {
		return err
	}
	r.pool = pool

	rootDataPath, err := ioutil.TempDir("", "")
	if err != nil {
		return err
	}
	r.rootDataPath = rootDataPath
	r.t.Cleanup(func() {
		_ = os.RemoveAll(r.rootDataPath)
	})

	network, err := r.pool.Client.CreateNetwork(docker.CreateNetworkOptions{
		Name:           NETWORK_NAME,
		Options:        map[string]interface{}{},
		Labels:         map[string]string{NETWORK_LABEL_KEY: r.t.Name()},
		CheckDuplicate: true,
		Internal:       false,
		EnableIPv6:     false,
		Context:        ctx,
	})
	if err != nil {
		return err
	}
	r.network = network
	r.t.Cleanup(func() {
		_ = r.pool.Client.RemoveNetwork(r.network.ID)
	})

	return nil
}
