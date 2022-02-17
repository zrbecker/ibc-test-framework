package util

import "github.com/ory/dockertest/docker"

type ContainerConfig struct {
	Repository string
	Version    string
	Bin        string
	Ports      map[docker.Port]struct{}
}

var (
	GaiaContainerConfig = ContainerConfig{
		Repository: "ghcr.io/strangelove-ventures/heighliner/gaia",
		Version:    "v5.0.7",
		Bin:        "gaiad",
		Ports: map[docker.Port]struct{}{
			"26656/tcp": {},
			"26657/tcp": {},
			"9090/tcp":  {},
			"1337/tcp":  {},
			"1234/tcp":  {},
		},
	}
)
