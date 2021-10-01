package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func dockerEventLoop(ctx context.Context, cli *client.Client, net *DockerNet) {
	log.Printf("Waiting for docker events")

	for {
		ctx, cancel := context.WithCancel(ctx)
		eventChan, errChan := cli.Events(ctx, types.EventsOptions{
			Filters: filters.NewArgs(
				filters.Arg("Type", "container"),
			),
		})

		quit := false
		for !quit {
			select {
			case event := <-eventChan:
				err := processEvent(ctx, cli, &event, net)
				if err != nil {
					log.Printf("Error: %v", err)
				}
			case err := <-errChan:
				log.Printf("Error reading events: %v", err)
				quit = true
			}
		}

		cancel()
		time.Sleep(5 * time.Second)
	}
}

func processEvent(ctx context.Context, cli *client.Client, event *events.Message, net *DockerNet) error {
	cid := event.Actor.ID
	if cid == "" {
		return nil
	}
	cname := event.Actor.Attributes["name"]
	image := event.Actor.Attributes["image"]
	desc := fmt.Sprintf("%s %s %s", cname, cid[0:10], image)
	att := Attachment{
		cid:   cid,
		cname: cname,
		net:   net,
	}

	if event.Action == "create" {
		log.Printf("Event: container created: %s", desc)
		return attachContainerToNet(ctx, cli, &att)
	}

	if event.Action == "start" {
		log.Printf("Event: container started: %s", desc)
		return routePorts(ctx, cli, &att)
	}

	if event.Action == "die" {
		log.Printf("Event: container exited: %s", desc)
		return unroutePorts(ctx, cli, &att)
	}

	return nil
}
