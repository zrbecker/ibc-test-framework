package chain

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
)

var (
	NODE_LABEL_KEY = "ibc-test"
)

type TestNodeContainer struct {
	N         *TestNode
	Config    *ContainerConfig
	Container *docker.Container
}

func NewTestNodeContainer(n *TestNode, config *ContainerConfig) *TestNodeContainer {
	nc := &TestNodeContainer{
		N:         n,
		Config:    config,
		Container: nil,
	}
	return nc
}

// HomeDir returns the home path in the container.
func (nc *TestNodeContainer) HomeDir() string {
	return filepath.Join("/home", nc.Config.Bin)
}

// InitHomeFolder initializes a home folder for the given node.
func (nc *TestNodeContainer) InitHomeFolder(ctx context.Context) error {
	command := []string{nc.Config.Bin, "init", nc.N.Name(),
		"--chain-id", nc.N.C.ChainId,
		"--home", nc.HomeDir(),
	}
	return nc.RunAndWait(ctx, command, "")
}

// CreateKey creates a key in the keyring backend test for the given node.
func (nc *TestNodeContainer) CreateKey(ctx context.Context, name string) error {
	command := []string{nc.Config.Bin, "keys", "add", name,
		"--keyring-backend", "test",
		"--output", "json",
		"--home", nc.HomeDir(),
	}
	return nc.RunAndWait(ctx, command, "")
}

// AddGenesisAccount adds a genesis account for each key.
func (nc *TestNodeContainer) AddGenesisAccount(ctx context.Context, address string) error {
	command := []string{nc.Config.Bin, "add-genesis-account", address, "1000000000000stake",
		"--home", nc.HomeDir(),
	}
	return nc.RunAndWait(ctx, command, "")
}

// Gentx generates the gentx for a given node.
func (nc *TestNodeContainer) Gentx(ctx context.Context, name string) error {
	command := []string{nc.Config.Bin, "gentx", VALIDATOR_KEY_NAME, "100000000000stake",
		"--keyring-backend", "test",
		"--home", nc.HomeDir(),
		"--chain-id", nc.N.C.ChainId,
	}
	return nc.RunAndWait(ctx, command, "")
}

// CollectGentxs runs collect gentxs on the node's home folders.
func (nc *TestNodeContainer) CollectGentxs(ctx context.Context) error {
	command := []string{nc.Config.Bin, "collect-gentxs",
		"--home", nc.HomeDir(),
	}
	return nc.RunAndWait(ctx, command, "")
}

// Start runs the start command for the chain.
func (nc *TestNodeContainer) Start(ctx context.Context) error {
	if nc.Container != nil {
		return fmt.Errorf("failed to start node %s, already exists", nc.N.Name())
	}
	command := []string{nc.Config.Bin, "start", "--home", nc.HomeDir()}
	resource, err := nc.Run(ctx, command, nc.N.Name())
	nc.Container = resource.Container
	return err
}

// Stop stops the container created by Start.
func (nc *TestNodeContainer) Stop(ctx context.Context) error {
	if nc.Container == nil {
		return fmt.Errorf("failed to stop node %s, does not exist", nc.N.Name())
	}
	if err := nc.N.C.Pool.Client.StopContainer(nc.Container.ID, 10); err != nil {
		return err
	}
	nc.Container = nil
	return nil
}

// Run runs a command in a docker container.
func (nc *TestNodeContainer) Run(
	ctx context.Context,
	cmd []string,
	containerName string,
) (*dockertest.Resource, error) {
	nc.N.C.T.Logf("{%s} -> '%s'", nc.N.Name(), strings.Join(cmd, " "))
	return nc.N.C.Pool.RunWithOptions(&dockertest.RunOptions{
		Name:         containerName,
		Hostname:     nc.N.Name(),
		Repository:   nc.Config.Repository,
		Tag:          nc.Config.Version,
		ExposedPorts: nc.Config.Ports,
		Cmd:          cmd,
		Labels:       map[string]string{NODE_LABEL_KEY: nc.N.C.T.Name()},
		NetworkID:    nc.N.C.Network.ID,
	}, func(config *docker.HostConfig) {
		config.Binds = []string{
			fmt.Sprintf("%s:%s", nc.N.HostHomeDir(), nc.HomeDir()),
		}
		config.PublishAllPorts = true
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
}

// Runs a command in a docker container and waits for it to exit.
func (nc *TestNodeContainer) RunAndWait(
	ctx context.Context,
	cmd []string,
	containerName string,
) error {
	resource, err := nc.Run(ctx, cmd, containerName)
	if err != nil {
		return err
	}
	if code, err := nc.N.C.Pool.Client.WaitContainerWithContext(resource.Container.ID, ctx); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("container returned non-zero error code: %d", code)
	}
	return nil
}

// GetHostPort returns a resource's published port with an address.
func (nc *TestNodeContainer) GetHostPort(portID string) string {
	if nc.Container == nil || nc.Container.NetworkSettings == nil {
		return ""
	}

	m, ok := nc.Container.NetworkSettings.Ports[docker.Port(portID)]
	if !ok || len(m) == 0 {
		return ""
	}

	ip := m[0].HostIP
	if ip == "0.0.0.0" {
		ip = "localhost"
	}
	return net.JoinHostPort(ip, m[0].HostPort)
}
