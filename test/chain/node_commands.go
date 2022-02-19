package chain

import (
	"context"
	"fmt"
	"strings"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"github.com/strangelove-ventures/ibc-test-framework/test/utils"
)

// InitHomeFolder initializes a home folder for the given node
func (n *TestNode) InitHomeFolder(ctx context.Context) error {
	command := []string{n.ContainerConfig.Bin, "init", n.Name(),
		"--chain-id", n.R.ChainId,
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

// CreateKey creates a key in the keyring backend test for the given node
func (n *TestNode) CreateKey(ctx context.Context, name string) error {
	command := []string{n.ContainerConfig.Bin, "keys", "add", name,
		"--keyring-backend", "test",
		"--output", "json",
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

// AddGenesisAccount adds a genesis account for each key
func (n *TestNode) AddGenesisAccount(ctx context.Context, address string) error {
	command := []string{n.ContainerConfig.Bin, "add-genesis-account", address, "1000000000000stake",
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

// Gentx generates the gentx for a given node
func (n *TestNode) Gentx(ctx context.Context, name string) error {
	command := []string{n.ContainerConfig.Bin, "gentx", VALIDATOR_KEY, "100000000000stake",
		"--keyring-backend", "test",
		"--home", n.HomeDir(),
		"--chain-id", n.R.ChainId,
	}
	return n.Execute(ctx, command)
}

// CollectGentxs runs collect gentxs on the node's home folders
func (n *TestNode) CollectGentxs(ctx context.Context) error {
	command := []string{n.ContainerConfig.Bin, "collect-gentxs",
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

func (n *TestNode) Start(ctx context.Context) error {
	if n.Container != nil {
		return fmt.Errorf("failed to start node %s, already exists", n.Name())
	}
	command := []string{n.ContainerConfig.Bin, "start", "--home", n.HomeDir()}
	resource, err := n.Run(ctx, command)
	n.Container = resource.Container
	n.R.T.Cleanup(func() {
		err := n.Stop(context.Background())
		if err != nil {
			n.R.T.Logf("failed to stop container %+v", err)
		}
	})
	return err
}

func (n *TestNode) Stop(ctx context.Context) error {
	if n.Container == nil {
		return fmt.Errorf("failed to stop node %s, does not exist", n.Name())
	}
	if err := n.R.Pool.Client.StopContainer(n.Container.ID, 10); err != nil {
		return err
	}
	return nil
}

func (n *TestNode) Run(ctx context.Context, cmd []string) (*dockertest.Resource, error) {
	n.R.T.Logf("{%s}[%s] -> '%s'", n.Name(), "", strings.Join(cmd, " "))
	return n.R.Pool.RunWithOptions(&dockertest.RunOptions{
		Name:         utils.RandLowerCaseLetterString(8),
		Hostname:     n.Name(),
		Repository:   n.ContainerConfig.Repository,
		Tag:          n.ContainerConfig.Version,
		ExposedPorts: n.ContainerConfig.Ports,
		Cmd:          cmd,
		Labels:       map[string]string{NODE_LABEL_KEY: n.R.T.Name()},
		NetworkID:    n.R.Network.ID,
	}, func(config *docker.HostConfig) {
		config.Binds = []string{
			fmt.Sprintf("%s:%s", n.HostHomeDir(), n.HomeDir()),
		}
		config.PublishAllPorts = true
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
}

func (n *TestNode) Execute(ctx context.Context, cmd []string) error {
	resource, err := n.Run(ctx, cmd)
	if err != nil {
		return err
	}
	code, err := n.R.Pool.Client.WaitContainerWithContext(resource.Container.ID, ctx)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("container returned non-zero error code: %d", code)
	}
	return nil
}
