package chain

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/avast/retry-go"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/ory/dockertest/docker"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
	libclient "github.com/tendermint/tendermint/rpc/jsonrpc/client"
)

var (
	NODE_LABEL_KEY = "ibc-test"
	VALIDATOR_KEY  = "validator"
)

type Node struct {
	R               *TestChain
	Id              int
	ContainerConfig *ContainerConfig
	IsValidator     bool
	Container       *docker.Container
	Client          *rpchttp.HTTP
}

func NewNode(r *TestChain, id int, containerConfig *ContainerConfig, isValidator bool) (*Node, error) {
	n := &Node{
		R:               r,
		Id:              id,
		ContainerConfig: containerConfig,
		IsValidator:     isValidator,
		Container:       nil,
		Client:          nil,
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

func (n *Node) PeerString() (string, error) {
	nodeID, err := n.NodeID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s@%s:26656", nodeID, n.Name()), nil
}

func (n *Node) TMConfigPath() string {
	return path.Join(n.HostHomeDir(), "config", "config.toml")
}

func (n *Node) SetValidatorConfig() error {
	config := tmconfig.DefaultConfig()

	peers, err := n.R.PeerString()
	if err != nil {
		return err
	}
	stdconfigchanges(config, peers)

	tmconfig.WriteConfigFile(n.TMConfigPath(), config)

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

// NewClient creates and assigns a new Tendermint RPC client to the TestNode
func (n *Node) NewClient(addr string) error {
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

// GetHostPort returns a resource's published port with an address.
func (n *Node) GetHostPort(portID string) string {
	if n.Container == nil || n.Container.NetworkSettings == nil {
		return ""
	}

	m, ok := n.Container.NetworkSettings.Ports[docker.Port(portID)]
	if !ok || len(m) == 0 {
		return ""
	}

	ip := m[0].HostIP
	if ip == "0.0.0.0" {
		ip = "localhost"
	}
	return net.JoinHostPort(ip, m[0].HostPort)
}

func (n *Node) SetupAndVerify(ctx context.Context) error {
	hostPort := n.GetHostPort("26657/tcp")
	n.R.T.Logf("{%s} RPC => %s", n.Name(), hostPort)

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
