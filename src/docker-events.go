package main

import (
	"context"
	"crypto/rand"
	"log"
	"net"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func startDockerEventLoop(ctx context.Context, net DockerNet) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}

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
		ip := randomIp(net.prefix).String()
		log.Printf("%s: attaching to network %s with IP %s", cname, net.name, ip)
		err := cli.NetworkConnect(ctx, net.id, cid, &network.EndpointSettings{
			GlobalIPv6Address: ip,
		})
		if err != nil {
			return err
		}
	}

	if event.Action == "die" {
		log.Printf(
			"Event: container stopped: %s %s %s",
			event.Actor.Attributes["name"],
			event.Actor.ID[0:10],
			event.Actor.Attributes["image"],
		)
	}

	return nil
}

func randomIp(prefix net.IPNet) net.IP {
	result := make([]byte, 16)
	rand.Read(result)
	for i := 0; i < 16; i++ {
		result[i] = (prefix.IP[i] & prefix.Mask[i]) | result[i]
	}
	return result
}
