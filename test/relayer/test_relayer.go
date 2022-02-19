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
) {
	rootDir, err := utils.CreateTmpDir()
	require.NoError(r.T, err)
	r.RootDir = rootDir

	chain1RPCAddress := fmt.Sprintf(
		"tcp://%s", node1.Container.GetHostPort("26657/tcp"))
	chain1Config := fmt.Sprintf(`{
	"chain-id": "%s",
	"rpc-addr": "%s",
	"account-prefix": "cosmos",
	"gas-adjustment": 1.5,
	"gas-prices": "0.001umuon",
	"trusting-period": "10m"
}`, node1.C.ChainID, chain1RPCAddress)

	chain2RPCAddress := fmt.Sprintf(
		"tcp://%s", node2.Container.GetHostPort("26657/tcp"))
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
	r.RunAndWait(ctx, command, "")

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
	command = []string{
		r.Bin, "chains", "add", "-f", path.Join(r.HomeDir(), "chain1_config.json")}
	r.RunAndWait(ctx, command, "")
	command = []string{
		r.Bin, "chains", "add", "-f", path.Join(r.HomeDir(), "chain2_config.json")}
	r.RunAndWait(ctx, command, "")

	// Create chain keys
	command = []string{
		r.Bin, "keys", "add", node1.C.ChainID,
		fmt.Sprintf("%s-key", node1.C.ChainID)}
	r.RunAndWait(ctx, command, "")
	command = []string{
		r.Bin, "keys", "add", node2.C.ChainID,
		fmt.Sprintf("%s-key", node2.C.ChainID)}
	r.RunAndWait(ctx, command, "")

	// Add keys to chain
	command = []string{
		r.Bin, "chains", "edit", node1.C.ChainID,
		"key", fmt.Sprintf("%s-key", node1.C.ChainID)}
	r.RunAndWait(ctx, command, "")
	command = []string{
		r.Bin, "chains", "edit", node2.C.ChainID,
		"key", fmt.Sprintf("%s-key", node2.C.ChainID)}
	r.RunAndWait(ctx, command, "")

	key1 := r.GetKey(node1.C.ChainID, fmt.Sprintf("%s-key", node1.C.ChainID))
	r.T.Logf("Key 1: %s", key1.GetAddress().String())
	key2 := r.GetKey(node2.C.ChainID, fmt.Sprintf("%s-key", node2.C.ChainID))
	r.T.Logf("Key 1: %s", key2.GetAddress().String())

	// Create path
	// command = []string{
	// 	r.Bin, "paths", "generate",
	// 	"chain-a", "chain-b", "transfer", "--port=transfer"}
	// r.RunAndWait(ctx, command, "")
}

func (r *TestRelayer) HostHomeDir() string {
	return path.Join(r.RootDir, "relayer")
}

func (r *TestRelayer) HomeDir() string {
	return path.Join("/root", ".relayer")
}

// Keybase returns the keyring for a given node
func (r *TestRelayer) Keybase(chainID string) keyring.Keyring {
	kr, err := keyring.New(
		"", keyring.BackendTest,
		path.Join(r.HostHomeDir(), "keys", chainID), os.Stdin)
	require.NoError(r.T, err)
	return kr
}

// GetKey gets a key, waiting until it is available
func (r *TestRelayer) GetKey(chainID string, name string) (info keyring.Info) {
	require.NoError(r.T, retry.Do(func() (err error) {
		kr := r.Keybase(chainID)
		info, err = kr.Key(name)
		return err
	}))
	return info
}

// Run runs a command in a docker container.
func (r *TestRelayer) Run(
	ctx context.Context,
	cmd []string,
	containerName string,
) *dockertest.Resource {
	r.T.Logf("{%s} -> '%s'", "relayer", strings.Join(cmd, " "))
	resource, err := r.Pool.RunWithOptions(&dockertest.RunOptions{
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
	require.NoError(r.T, err)
	return resource
}

// Runs a command in a docker container and waits for it to exit.
func (r *TestRelayer) RunAndWait(
	ctx context.Context,
	cmd []string,
	containerName string,
) {
	resource := r.Run(ctx, cmd, containerName)
	code, err := r.Pool.Client.WaitContainerWithContext(
		resource.Container.ID, ctx)
	require.NoError(r.T, err, "failed to wait for container")
	require.Equalf(
		r.T, code, 0, "container returned non-zero error code: %d", code)
}
