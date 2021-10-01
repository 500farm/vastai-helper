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
	).String()
	staticPrefix = kingpin.Flag(
		"static-prefix",
		"Static IPv6 prefix for address assignment (length from /48 to /96).",
	).String()
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

	if *dhcpInterface == "" && *staticPrefix == "" {
		log.Printf("Please specify either --dhcp-interface or --static-prefix")
		os.Exit(1)
	}
	if *dhcpInterface != "" && *staticPrefix != "" {
		log.Printf("Please specify either --dhcp-interface or --static-prefix, not both")
		os.Exit(1)
	}

	ctx := context.Background()

	var netConf NetConf
	var err error
	if *dhcpInterface != "" {
		netConf, err = startDhcp(ctx, *dhcpInterface)
	} else {
		netConf, err = staticNetConf(*staticPrefix)
	}
	if err != nil {
		log.Printf("Error: %v", err)
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
