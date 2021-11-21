package main

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"

	apiPlugin "vastai-helper/src/plugins/api"
	autoPrunePlugin "vastai-helper/src/plugins/autoprune"
	netAttachPlugin "vastai-helper/src/plugins/netattach"
)

func createDockerClient() *client.Client {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}
	return cli
}

var plugins []Plugin

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	cli := createDockerClient()
	ctx := context.Background()
	stateDir := "/var/lib/vastai-helper/"

	plugins = []Plugin{
		autoPrunePlugin.NewPlugin(ctx, cli, stateDir),
		apiPlugin.NewPlugin(ctx, cli),
		netAttachPlugin.NewPlugin(ctx, cli, stateDir),
	}

	if err := discoverContainers(ctx, cli); err != nil {
		log.Fatal(err)
	}
	for _, plugin := range plugins {
		if err := plugin.Start(); err != nil {
			log.Fatal(err)
		}
	}

	dockerEventLoop(ctx, cli)
}
