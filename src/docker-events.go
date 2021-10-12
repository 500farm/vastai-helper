package main

import (
	"context"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func dockerEventLoop(ctx context.Context, cli *client.Client, net *DockerNet) {
	log.Info("Waiting for docker events")

	for {
		ctx, cancel := context.WithCancel(ctx)
		eventChan, errChan := cli.Events(ctx, types.EventsOptions{
			Filters: filters.NewArgs(
				filters.Arg("Type", "container"),
				filters.Arg("Type", "image"),
			),
		})

		quit := false
		for !quit {
			select {
			case event := <-eventChan:
				err := processEvent(ctx, cli, &event, net)
				if err != nil {
					log.Error(err)
				}
			case err := <-errChan:
				log.Error("Error reading docker events: ", err)
				quit = true
			}
		}

		cancel()
		time.Sleep(5 * time.Second)
	}
}

func processEvent(ctx context.Context, cli *client.Client, event *events.Message, net *DockerNet) error {
	if event.Type == "container" {
		cid := event.Actor.ID
		if cid == "" {
			return nil
		}
		cname := event.Actor.Attributes["name"]
		image := event.Actor.Attributes["image"]
		if strings.HasPrefix(image, "sha256:") {
			// ignore temporary containers
			return nil
		}
		att := Attachment{
			cid:   cid,
			cname: cname,
			net:   net,
		}
		logger := func() *log.Entry {
			return log.WithFields(log.Fields{
				"event": event.Action,
				"cid":   cid[0:12],
				"cname": cname,
				"image": image,
			})
		}
		if event.Action == "create" {
			logger().Info("Container created")
			return attachContainerToNet(ctx, cli, &att)
		}
		if event.Action == "start" {
			logger().Info("Container started")
			return routePorts(ctx, cli, &att)
		}
		if event.Action == "die" {
			logger().Info("Container exited")
			return unroutePorts(ctx, cli, &att)
		}
		if event.Action == "destroy" {
			logger().Info("Container destroyed")
			return nil
		}
		if strings.HasPrefix(event.Action, "exec_start: ") {
			logger().
				WithFields(log.Fields{"event": "exec", "cmd": strings.TrimSpace(event.Action[12:])}).
				Info("Container exec")
			return nil
		}
	}

	if event.Type == "image" {
		if event.Action == "pull" {
			log.WithFields(log.Fields{
				"event": "pull",
				"image": event.Actor.ID,
			}).Info("Docker image pulled")
			return nil
		}
	}

	return nil
}
