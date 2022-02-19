package util

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/avast/retry-go"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/tendermint/tendermint/p2p"
)

var (
	NODE_LABEL_KEY = "ibc-test"
	VALIDATOR_KEY  = "validator"
)

type Node struct {
	R               *ChainRunner
	Id              int
	ContainerConfig *ContainerConfig
	IsValidator     bool
}

func NewNode(r *ChainRunner, id int, containerConfig *ContainerConfig, isValidator bool) (*Node, error) {
	n := &Node{
		R:               r,
		Id:              id,
		ContainerConfig: containerConfig,
		IsValidator:     isValidator,
	}
	if err := n.initHostEnv(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *Node) initHostEnv() error {
	if err := os.MkdirAll(n.HostHomeDir(), 0755); err != nil {
		return err
	}

	return nil
}

func (n *Node) Name() string {
	return fmt.Sprintf("node-%s-%d", n.R.T.Name(), n.Id)
}

func (n *Node) HostHomeDir() string {
	return filepath.Join(n.R.RootDataPath, n.Name())
}

func (n *Node) HomeDir() string {
	return filepath.Join("/home", n.ContainerConfig.Bin)
}

// Keybase returns the keyring for a given node
func (n *Node) Keybase() (keyring.Keyring, error) {
	kr, err := keyring.New("", keyring.BackendTest, n.HostHomeDir(), os.Stdin)
	if err != nil {
		return nil, err
	}
	return kr, nil
}

// GetKey gets a key, waiting until it is available
func (n *Node) GetKey(name string) (info keyring.Info, err error) {
	return info, retry.Do(func() (err error) {
		kr, err := n.Keybase()
		if err != nil {
			return err
		}
		info, err = kr.Key(name)
		return err
	})
}

func (n *Node) Initialize(ctx context.Context) error {
	if err := n.InitHomeFolder(ctx); err != nil {
		return err
	}

	if n.IsValidator {
		if err := n.CreateKey(ctx, VALIDATOR_KEY); err != nil {
			return err
		}
	}

	return nil
}

// NodeID returns the node of a given node
func (n *Node) NodeID() (string, error) {
	nodeKey, err := p2p.LoadNodeKey(path.Join(n.HostHomeDir(), "config", "node_key.json"))
	if err != nil {
		return "", err
	}
	return string(nodeKey.ID()), nil
}

func (n *Node) GenesisFilePath() string {
	return path.Join(n.HostHomeDir(), "config", "genesis.json")
}

func (n *Node) CreateGenesisTx(ctx context.Context) error {
	key, err := n.GetKey(VALIDATOR_KEY)
	if err != nil {
		return err
	}
	if err := n.AddGenesisAccount(ctx, key.GetAddress().String()); err != nil {
		return err
	}

	if err := n.Gentx(ctx, VALIDATOR_KEY); err != nil {
		return err
	}

	return nil
}
