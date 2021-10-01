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

func readDockerEvents(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}

	for {
		ctx, cancel := context.WithCancel(ctx)
		eventChan, errChan := cli.Events(ctx, types.EventsOptions{
			Filters: filters.NewArgs(
				filters.Arg("Type", "container"),
			),
		})

		select {
		case event := <-eventChan:
			log.Printf("Event: %v", event)
			err := processEvent(ctx, event)
			if err != nil {
				log.Printf("Error: %v", err)
			}
		case err := <-errChan:
			log.Printf("Error reading events: %v", err)
			time.Sleep(5 * time.Second)
			break
		}

		cancel()
	}
}

func processEvent(ctx context.Context, event events.Message) error {
	return nil
}
