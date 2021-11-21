package main

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"

	apiPlugin "./plugins/api"
	autoPrunePlugin "./plugins/auto-prune"
	netAttachPlugin "./plugins/net-attach"
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

	plugins = []Plugin{
		autoPrunePlugin.NewAutoPrunePlugin(ctx, cli),
		apiPlugin.NewApiPlugin(ctx, cli),
		netAttachPlugin.NewNetAttachPlugin(ctx, cli),
	}

	for _, plugin := range plugins {
		if err := plugin.Start(); err != nil {
			log.Fatal(err)
		}
	}

	dockerEventLoop(ctx, cli)
}
