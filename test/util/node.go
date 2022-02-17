package util

import (
	"fmt"
	"os"
	"path/filepath"
)

type Node struct {
	r               *ChainRunner
	id              int
	containerConfig *ContainerConfig
}

func NewNode(r *ChainRunner, id int, containerConfig *ContainerConfig) (*Node, error) {
	n := &Node{
		r:               r,
		id:              id,
		containerConfig: containerConfig,
	}
	if err := n.initHostEnv(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *Node) Name() string {
	return fmt.Sprintf("node-%s-%d", n.r.t.Name(), n.id)
}

func (n *Node) HostDataPath() string {
	return filepath.Join(n.r.rootDataPath, n.Name())
}

func (n *Node) initHostEnv() error {
	if err := os.MkdirAll(n.HostDataPath(), 0755); err != nil {
		return err
	}
	return nil
}
