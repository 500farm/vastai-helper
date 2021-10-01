package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	dhcpInterface = kingpin.Flag(
		"dhcp-interface",
		"Interface where to listen for DHCP-PD.",
	).Required().String()

	netConf NetConf
)

func dhcpRenew(ctx context.Context) error {
	conf, err := receiveConfWithDhcp(ctx, *dhcpInterface)
	if err != nil {
		log.Printf("DHCPv6 error: %v", err)
		return err
	}
	log.Printf("Received network configuration: %s", conf.String())
	netConf = conf
	return nil
}

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
	err := dhcpRenew(ctx)
	if err != nil {
		os.Exit(1)
	}

	// DHCP renew timer
	go func() {
		for {
			time.Sleep(netConf.preferredLifetime)
			for {
				if err := dhcpRenew(ctx); err == nil {
					break
				}
				delay := 15 * time.Minute
				log.Printf("Will retry in %s", delay.String())
				time.Sleep(delay)
			}
		}
	}()

	cli := createDockerClient()

	dockerNet, err := selectOrCreateDockerNet(ctx, cli, netConf)
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}

	err = startDockerEventLoop(ctx, cli, dockerNet)
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
