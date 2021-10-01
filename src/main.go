package main

import (
	"context"
	"log"
	"os"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	dhcpInterface = kingpin.Flag(
		"dhcp-interface",
		"Interface where to listen for DHCP-PD.",
	).Required().String()
)

func createDockerClient() *client.Client {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
	return cli
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	ctx := context.Background()
	netConf, err := startDhcp(ctx, *dhcpInterface)
	if err != nil {
		os.Exit(1)
	}

	cli := createDockerClient()
	dockerNet, err := selectOrCreateDockerNet(ctx, cli, &netConf)
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}

	dockerEventLoop(ctx, cli, &dockerNet)
}
