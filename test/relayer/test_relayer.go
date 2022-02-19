package relayer

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/avast/retry-go"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"github.com/strangelove-ventures/ibc-test-framework/test/chain"
	"github.com/strangelove-ventures/ibc-test-framework/test/utils"
	"github.com/stretchr/testify/require"
)

type TestRelayer struct {
	T          *testing.T
	Pool       *dockertest.Pool
	Repository string
	Version    string
	Bin        string
	RootDir    string
}

func NewTestRelayer(
	t *testing.T,
	pool *dockertest.Pool,
	repository string,
	version string,
	bin string,
) *TestRelayer {
	return &TestRelayer{
		T:          t,
		Pool:       pool,
		Repository: repository,
		Version:    version,
		Bin:        bin,
	}
}

func (r *TestRelayer) Initialize(
	ctx context.Context, node1 *chain.TestNode, node2 *chain.TestNode,
) error {
	rootDir, err := utils.CreateTmpDir()
	if err != nil {
		return err
	}
	r.RootDir = rootDir

	chain1RPCAddress := fmt.Sprintf("tcp://%s", node1.Container.GetHostPort("26657/tcp"))
	chain1Config := fmt.Sprintf(`{
	"chain-id": "%s",
	"rpc-addr": "%s",
	"account-prefix": "cosmos",
	"gas-adjustment": 1.5,
	"gas-prices": "0.001umuon",
	"trusting-period": "10m"
}`, node1.C.ChainID, chain1RPCAddress)

	chain2RPCAddress := fmt.Sprintf("tcp://%s", node2.Container.GetHostPort("26657/tcp"))
	chain2Config := fmt.Sprintf(`{
	"chain-id": "%s",
	"rpc-addr": "%s",
	"account-prefix": "cosmos",
	"gas-adjustment": 1.5,
	"gas-prices": "0.001umuon",
	"trusting-period": "10m"
}`, node2.C.ChainID, chain2RPCAddress)

	// Initialize rly config
	command := []string{r.Bin, "config", "init", "--home", r.HomeDir()}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}

	require.NoError(r.T, ioutil.WriteFile(
		path.Join(r.HostHomeDir(), "chain1_config.json"),
		[]byte(chain1Config),
		0644,
	))
	require.NoError(r.T, ioutil.WriteFile(
		path.Join(r.HostHomeDir(), "chain2_config.json"),
		[]byte(chain2Config),
		0644,
	))

	// Add chain configs
	command = []string{r.Bin, "chains", "add", "-f", path.Join(r.HomeDir(), "chain1_config.json")}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}
	command = []string{r.Bin, "chains", "add", "-f", path.Join(r.HomeDir(), "chain2_config.json")}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}

	// Create chain keys
	command = []string{r.Bin, "keys", "add", node1.C.ChainID, fmt.Sprintf("%s-key", node1.C.ChainID)}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}
	command = []string{r.Bin, "keys", "add", node2.C.ChainID, fmt.Sprintf("%s-key", node2.C.ChainID)}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}

	// Add keys to chain
	command = []string{r.Bin, "chains", "edit", node1.C.ChainID, "key", fmt.Sprintf("%s-key", node1.C.ChainID)}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}
	command = []string{r.Bin, "chains", "edit", node2.C.ChainID, "key", fmt.Sprintf("%s-key", node2.C.ChainID)}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}

	key1, err := r.GetKey(node1.C.ChainID, fmt.Sprintf("%s-key", node1.C.ChainID))
	if err != nil {
		return err
	}
	r.T.Logf("Key 1: %s", key1)
	key2, err := r.GetKey(node2.C.ChainID, fmt.Sprintf("%s-key", node2.C.ChainID))
	if err != nil {
		return err
	}
	r.T.Logf("Key 1: %s", key2)

	// Create path
	command = []string{r.Bin, "paths", "generate", "chain-a", "chain-b", "transfer", "--port=transfer"}
	if err := r.RunAndWait(ctx, command, ""); err != nil {
		return err
	}

	return nil
}

func (r *TestRelayer) HostHomeDir() string {
	return path.Join(r.RootDir, "relayer")
}

func (r *TestRelayer) HomeDir() string {
	return path.Join("/root", ".relayer")
}

// Keybase returns the keyring for a given node
func (r *TestRelayer) Keybase(chainID string) (keyring.Keyring, error) {
	kr, err := keyring.New("", keyring.BackendTest, path.Join(r.HostHomeDir(), "keys", chainID), os.Stdin)
	if err != nil {
		return nil, err
	}
	return kr, nil
}

// GetKey gets a key, waiting until it is available
func (r *TestRelayer) GetKey(chainID string, name string) (info keyring.Info, err error) {
	return info, retry.Do(func() (err error) {
		kr, err := r.Keybase(chainID)
		if err != nil {
			return err
		}
		info, err = kr.Key(name)
		return err
	})
}

// Run runs a command in a docker container.
func (r *TestRelayer) Run(
	ctx context.Context,
	cmd []string,
	containerName string,
) (*dockertest.Resource, error) {
	r.T.Logf("{%s} -> '%s'", "relayer", strings.Join(cmd, " "))
	return r.Pool.RunWithOptions(&dockertest.RunOptions{
		Name:       containerName,
		Repository: r.Repository,
		Tag:        r.Version,
		Cmd:        cmd,
	}, func(config *docker.HostConfig) {
		config.Binds = []string{
			fmt.Sprintf("%s:%s", r.HostHomeDir(), r.HomeDir()),
		}
		config.PublishAllPorts = true
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
}

// Runs a command in a docker container and waits for it to exit.
func (r *TestRelayer) RunAndWait(
	ctx context.Context,
	cmd []string,
	containerName string,
) error {
	resource, err := r.Run(ctx, cmd, containerName)
	if err != nil {
		return err
	}
	if code, err := r.Pool.Client.WaitContainerWithContext(resource.Container.ID, ctx); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("container returned non-zero error code: %d", code)
	}
	return nil
}
