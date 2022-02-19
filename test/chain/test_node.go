package chain

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/avast/retry-go"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	libclient "github.com/tendermint/tendermint/rpc/jsonrpc/client"
)

var (
	VALIDATOR_KEY_NAME = "validator"
)

type TestNode struct {
	C         *TestChain
	ID        int
	Container *TestNodeContainer
	Client    *rpchttp.HTTP
}

func NewTestNode(c *TestChain) (*TestNode, error) {
	n := &TestNode{
		C:         c,
		ID:        c.NextNodeID,
		Container: nil,
		Client:    nil,
	}
	n.Container = NewTestNodeContainer(n, c.ContainerConfig)
	if err := n.initHostEnv(); err != nil {
		return nil, err
	}
	c.NextNodeID += 1
	c.Nodes = append(c.Nodes, n)
	return n, nil
}

func (n *TestNode) Name() string {
	return fmt.Sprintf("node-%s-%s-%d", n.C.T.Name(), n.C.ChainID, n.ID)
}

// HostHomeDir returns the host home directory that is mounted on the docker container
func (n *TestNode) HostHomeDir() string {
	return filepath.Join(n.C.RootDataPath, n.Name())
}

// Keybase returns the keyring for a given node
func (n *TestNode) Keybase() (keyring.Keyring, error) {
	kr, err := keyring.New("", keyring.BackendTest, n.HostHomeDir(), os.Stdin)
	if err != nil {
		return nil, err
	}
	return kr, nil
}

// GetKey gets a key, waiting until it is available
func (n *TestNode) GetKey(name string) (info keyring.Info, err error) {
	return info, retry.Do(func() (err error) {
		kr, err := n.Keybase()
		if err != nil {
			return err
		}
		info, err = kr.Key(name)
		return err
	})
}

// Initialize prepares the node before starting
func (n *TestNode) Initialize(ctx context.Context) error {
	if err := n.Container.InitHomeFolder(ctx); err != nil {
		return err
	}

	if err := n.Container.CreateKey(ctx, VALIDATOR_KEY_NAME); err != nil {
		return err
	}

	return nil
}

// TestNodeID returns the node ID of a given node
func (n *TestNode) TestNodeID() (string, error) {
	nodeKey, err := p2p.LoadNodeKey(path.Join(n.HostHomeDir(), "config", "node_key.json"))
	if err != nil {
		return "", err
	}
	return string(nodeKey.ID()), nil
}

func (n *TestNode) GenesisFilePath() string {
	return path.Join(n.HostHomeDir(), "config", "genesis.json")
}

func (n *TestNode) CreateGenesisTx(ctx context.Context) error {
	key, err := n.GetKey(VALIDATOR_KEY_NAME)
	if err != nil {
		return err
	}
	if err := n.Container.AddGenesisAccount(ctx, key.GetAddress().String()); err != nil {
		return err
	}

	if err := n.Container.Gentx(ctx, VALIDATOR_KEY_NAME); err != nil {
		return err
	}

	return nil
}

func (n *TestNode) PeerString() (string, error) {
	nodeID, err := n.TestNodeID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s@%s:26656", nodeID, n.Name()), nil
}

func (n *TestNode) TMConfigPath() string {
	return path.Join(n.HostHomeDir(), "config", "config.toml")
}

func (n *TestNode) SetValidatorConfig() error {
	config := tmconfig.DefaultConfig()

	peers, err := n.C.PeerString()
	if err != nil {
		return err
	}
	stdconfigchanges(config, peers)

	config.Moniker = n.Name()

	tmconfig.WriteConfigFile(n.TMConfigPath(), config)

	return nil
}

// NewClient creates and assigns a new Tendermint RPC client to the TestTestNode
func (n *TestNode) NewClient(addr string) error {
	httpClient, err := libclient.DefaultHTTPClient(addr)
	if err != nil {
		return err
	}

	httpClient.Timeout = 10 * time.Second
	rpcClient, err := rpchttp.NewWithClient(addr, "/websocket", httpClient)
	if err != nil {
		return err
	}

	n.Client = rpcClient
	return nil
}

func (n *TestNode) Start(ctx context.Context) error {
	if err := n.SetValidatorConfig(); err != nil {
		return err
	}

	if err := n.Container.Start(ctx); err != nil {
		return err
	}

	hostPort := n.Container.GetHostPort("26657/tcp")
	n.C.T.Logf("{%s} RPC => %s", n.Name(), hostPort)

	if err := n.NewClient(fmt.Sprintf("tcp://%s", hostPort)); err != nil {
		return err
	}

	time.Sleep(5 * time.Second)
	return retry.Do(func() error {
		stat, err := n.Client.Status(ctx)
		if err != nil {
			return err
		}

		// TODO: reenable this check, having trouble with it for some reason
		if stat != nil && stat.SyncInfo.CatchingUp {
			return fmt.Errorf("still catching up: height(%d) catching-up(%t)",
				stat.SyncInfo.LatestBlockHeight, stat.SyncInfo.CatchingUp)
		}
		return nil
	}, retry.DelayType(retry.BackOffDelay))
}

func (n *TestNode) WaitForHeight(ctx context.Context, height int64) error {
	return retry.Do(func() error {
		stat, err := n.Client.Status(ctx)
		if err != nil {
			return err
		}

		if stat.SyncInfo.CatchingUp || stat.SyncInfo.LatestBlockHeight < height {
			return fmt.Errorf("node still under block %d: %d", height, stat.SyncInfo.LatestBlockHeight)
		}
		n.C.T.Logf("{%s} => reached block %d\n", n.Name(), height)
		return nil
		// TODO: setup backup delay here
	}, retry.DelayType(retry.BackOffDelay), retry.Attempts(15))
}

func (n *TestNode) CopyGenesisFileFromNode(other *TestNode) error {
	genesis, err := ioutil.ReadFile(other.GenesisFilePath())
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(n.GenesisFilePath(), genesis, 0644); err != nil {
		return err
	}

	return nil
}

func (n *TestNode) initHostEnv() error {
	if err := os.MkdirAll(n.HostHomeDir(), 0755); err != nil {
		return err
	}

	return nil
}

func stdconfigchanges(cfg *tmconfig.Config, peers string) {
	// turn down blocktimes to make the chain faster
	cfg.Consensus.TimeoutCommit = 3 * time.Second
	cfg.Consensus.TimeoutPropose = 3 * time.Second

	// Open up rpc address
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"

	// Allow for some p2p weirdness
	cfg.P2P.AllowDuplicateIP = true
	cfg.P2P.AddrBookStrict = false

	// Set log level to info
	cfg.BaseConfig.LogLevel = "info"

	// set persistent peer nodes
	cfg.P2P.PersistentPeers = peers
}
