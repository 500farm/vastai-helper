package main

import (
	"context"

	log "github.com/sirupsen/logrus"

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
		log.Fatal(err)
	}
	return cli
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	if *dhcpInterface == "" && *staticPrefix == "" {
		log.Fatal("Please specify either --dhcp-interface or --static-prefix")
	}
	if *dhcpInterface != "" && *staticPrefix != "" {
		log.Fatal("Please specify either --dhcp-interface or --static-prefix, not both")
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
		log.Fatal(err)
	}

	cli := createDockerClient()
	dockerNet, err := selectOrCreateDockerNet(ctx, cli, &netConf)
	if err != nil {
		log.Fatal(err)
	}

	dockerEventLoop(ctx, cli, &dockerNet)
}
