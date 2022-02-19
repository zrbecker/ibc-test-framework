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
	"github.com/stretchr/testify/require"
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

func NewTestNode(c *TestChain) *TestNode {
	n := &TestNode{
		C:         c,
		ID:        c.NextNodeID,
		Container: nil,
		Client:    nil,
	}
	n.Container = NewTestNodeContainer(n, c.ContainerConfig)
	n.initHostEnv()
	c.NextNodeID += 1
	c.Nodes = append(c.Nodes, n)
	return n
}

func (n *TestNode) Name() string {
	return fmt.Sprintf("node-%s-%s-%d", n.C.T.Name(), n.C.ChainID, n.ID)
}

// HostHomeDir returns the host home directory that is mounted on the docker
// container
func (n *TestNode) HostHomeDir() string {
	return filepath.Join(n.C.RootDataPath, n.Name())
}

// Keybase returns the keyring for a given node
func (n *TestNode) Keybase() keyring.Keyring {
	kr, err := keyring.New("", keyring.BackendTest, n.HostHomeDir(), os.Stdin)
	require.NoError(n.C.T, err, "could not retrieve keyring")
	return kr
}

// GetKey gets a key, waiting until it is available
func (n *TestNode) GetKey(name string) (info keyring.Info) {
	require.NoErrorf(n.C.T, retry.Do(func() (err error) {
		kr := n.Keybase()
		info, err = kr.Key(name)
		return err
	}), "could not retrieve %s key", name)
	return info
}

// Initialize prepares the node before starting
func (n *TestNode) Initialize(ctx context.Context) {
	n.Container.InitHomeFolder(ctx)
	n.Container.CreateKey(ctx, VALIDATOR_KEY_NAME)
}

// TestNodeID returns the node ID of a given node
func (n *TestNode) TestNodeID() string {
	nodeKey, err := p2p.LoadNodeKey(
		path.Join(n.HostHomeDir(), "config", "node_key.json"))
	require.NoError(n.C.T, err, "failed to retrieve node id")
	return string(nodeKey.ID())
}

func (n *TestNode) GenesisFilePath() string {
	return path.Join(n.HostHomeDir(), "config", "genesis.json")
}

func (n *TestNode) CreateGenesisTx(ctx context.Context) {
	key := n.GetKey(VALIDATOR_KEY_NAME)
	n.Container.AddGenesisAccount(ctx, key.GetAddress().String())
	n.Container.Gentx(ctx, VALIDATOR_KEY_NAME)
}

func (n *TestNode) PeerString() string {
	nodeID := n.TestNodeID()
	return fmt.Sprintf("%s@%s:26656", nodeID, n.Name())
}

func (n *TestNode) TMConfigPath() string {
	return path.Join(n.HostHomeDir(), "config", "config.toml")
}

func (n *TestNode) SetValidatorConfig() {
	config := tmconfig.DefaultConfig()

	peers := n.C.PeerString()
	stdconfigchanges(config, peers)

	config.Moniker = n.Name()

	tmconfig.WriteConfigFile(n.TMConfigPath(), config)
}

// NewClient creates and assigns a new Tendermint RPC client to the TestTestNode
func (n *TestNode) NewClient(addr string) {
	httpClient, err := libclient.DefaultHTTPClient(addr)
	require.NoError(n.C.T, err, "failed to create http client")

	httpClient.Timeout = 10 * time.Second
	rpcClient, err := rpchttp.NewWithClient(addr, "/websocket", httpClient)
	require.NoError(n.C.T, err, "failed to create rpc client")

	n.Client = rpcClient
}

func (n *TestNode) Start(ctx context.Context) {
	n.SetValidatorConfig()
	n.Container.Start(ctx)

	hostPort := n.Container.GetHostPort("26657/tcp")
	n.C.T.Logf("{%s} RPC => %s", n.Name(), hostPort)

	n.NewClient(fmt.Sprintf("tcp://%s", hostPort))

	time.Sleep(5 * time.Second)
	require.NoErrorf(n.C.T, retry.Do(func() error {
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
	}, retry.DelayType(retry.BackOffDelay)), "failed to start node %s", n.Name())
}

func (n *TestNode) WaitForHeight(ctx context.Context, height int64) {
	require.NoErrorf(n.C.T, retry.Do(func() error {
		stat, err := n.Client.Status(ctx)
		if err != nil {
			return err
		}

		if stat.SyncInfo.CatchingUp || stat.SyncInfo.LatestBlockHeight < height {
			return fmt.Errorf(
				"node still under block %d: %d",
				height, stat.SyncInfo.LatestBlockHeight)
		}
		n.C.T.Logf("{%s} => reached block %d\n", n.Name(), height)
		return nil
		// TODO: setup backup delay here
	}, retry.DelayType(retry.BackOffDelay), retry.Attempts(15)),
		"failed to achieve height %s", n.Name)
}

func (n *TestNode) CopyGenesisFileFromNode(other *TestNode) {
	genesis, err := ioutil.ReadFile(other.GenesisFilePath())
	require.NoErrorf(
		n.C.T, err, "failed to read genesis file %s", other.GenesisFilePath())

	require.NoErrorf(
		n.C.T, ioutil.WriteFile(n.GenesisFilePath(), genesis, 0644),
		"failed to write genesis file %s", n.GenesisFilePath())
}

func (n *TestNode) initHostEnv() {
	require.NoErrorf(
		n.C.T, os.MkdirAll(n.HostHomeDir(), 0755),
		"failed to create host home dir %s", n.HostHomeDir())
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
