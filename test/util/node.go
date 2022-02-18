package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
)

var (
	NODE_LABEL_KEY = "ibc-test"
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

func (n *Node) Initialize(ctx context.Context) error {
	command := []string{n.ContainerConfig.Bin, "init", n.Name(),
		"--chain-id", n.R.ChainId,
		"--home", n.HomeDir(),
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
