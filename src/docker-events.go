package main

import (
	"context"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func startDockerEventLoop(ctx context.Context, cli *client.Client, net DockerNet) error {
	log.Printf("Waiting for docker events")

	for {
		ctx, cancel := context.WithCancel(ctx)
		eventChan, errChan := cli.Events(ctx, types.EventsOptions{
			Filters: filters.NewArgs(
				filters.Arg("Type", "container"),
			),
		})

		select {
		case event := <-eventChan:
			err := processEvent(ctx, cli, event, net)
			if err != nil {
				log.Printf("Error: %v", err)
			}
		case err := <-errChan:
			log.Printf("Error reading events: %v", err)
			break
		}

		cancel()
		time.Sleep(5 * time.Second)
	}
}

func processEvent(ctx context.Context, cli *client.Client, event events.Message, net DockerNet) error {
	if event.Action == "create" {
		cid := event.Actor.ID
		cname := event.Actor.Attributes["name"]
		log.Printf(
			"Event: container started: %s %s %s",
			cname,
			cid[0:10],
			event.Actor.Attributes["image"],
		)
		return attachContainerToNet(ctx, cli, cid, cname, net)
	}

	if event.Action == "die" {
		log.Printf(
			"Event: container stopped: %s %s %s",
			event.Actor.Attributes["name"],
			event.Actor.ID[0:10],
			event.Actor.Attributes["image"],
		)
		return cleanupContainer(ctx, cli, event.Actor.ID)
	}

	return nil
}
