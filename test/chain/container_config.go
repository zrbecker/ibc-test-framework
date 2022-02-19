package util

type ContainerConfig struct {
	Repository string
	Version    string
	Bin        string
	Ports      []string
}

var (
	GaiaContainerConfig = ContainerConfig{
		Repository: "ghcr.io/strangelove-ventures/heighliner/gaia",
		Version:    "v5.0.7",
		Bin:        "gaiad",
		Ports: []string{
			"26656/tcp",
			"26657/tcp",
			"9090/tcp",
			"1337/tcp",
			"1234/tcp",
		},
	}
)
