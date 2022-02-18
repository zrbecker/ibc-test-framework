package util

import (
	"context"
	"fmt"
	"strings"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
)

// InitHomeFolder initializes a home folder for the given node
func (n *Node) InitHomeFolder(ctx context.Context) error {
	command := []string{n.ContainerConfig.Bin, "init", n.Name(),
		"--chain-id", n.R.ChainId,
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

// CreateKey creates a key in the keyring backend test for the given node
func (n *Node) CreateKey(ctx context.Context, name string) error {
	command := []string{n.ContainerConfig.Bin, "keys", "add", name,
		"--keyring-backend", "test",
		"--output", "json",
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

// AddGenesisAccount adds a genesis account for each key
func (n *Node) AddGenesisAccount(ctx context.Context, address string) error {
	command := []string{n.ContainerConfig.Bin, "add-genesis-account", address, "1000000000000stake",
		"--home", n.HomeDir(),
	}
	return n.Execute(ctx, command)
}

// Gentx generates the gentx for a given node
func (n *Node) Gentx(ctx context.Context, name string) error {
	command := []string{n.ContainerConfig.Bin, "gentx", VALIDATOR_KEY, "100000000000stake",
		"--keyring-backend", "test",
		"--home", n.HomeDir(),
		"--chain-id", n.R.ChainId,
	}
	return n.Execute(ctx, command)
}

func (n *Node) Execute(ctx context.Context, cmd []string) error {
	// TODO(zrbecker): Should a container have a name and hostname? And should it be random?
	n.R.T.Logf("{%s}[%s] -> '%s'", n.Name(), "", strings.Join(cmd, " "))

	resource, err := n.R.Pool.RunWithOptions(&dockertest.RunOptions{
		Repository:   n.ContainerConfig.Repository,
		Tag:          n.ContainerConfig.Version,
		ExposedPorts: n.ContainerConfig.Ports,
		Cmd:          cmd,
		Labels:       map[string]string{NODE_LABEL_KEY: n.R.T.Name()},
	}, func(config *docker.HostConfig) {
		config.Binds = []string{
			fmt.Sprintf("%s:%s", n.HostHomeDir(), n.HomeDir()),
		}
		config.PublishAllPorts = true
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
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
