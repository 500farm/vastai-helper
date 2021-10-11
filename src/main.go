package main

import (
	"context"
	"os"

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
	test = kingpin.Flag(
		"test",
		"Perform a self-test of a running daemon.",
	).Bool()
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

	ctx := context.Background()

	if *test {
		if selfTest(ctx, createDockerClient()) {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	}

	if *dhcpInterface == "" && *staticPrefix == "" {
		log.Fatal("Please specify either --dhcp-interface or --static-prefix")
	}
	if *dhcpInterface != "" && *staticPrefix != "" {
		log.Fatal("Please specify either --dhcp-interface or --static-prefix, not both")
	}

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
