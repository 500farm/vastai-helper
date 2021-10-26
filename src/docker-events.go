package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func dockerEventLoop(ctx context.Context, cli *client.Client, net *DockerNet) {
	retry := 5 * time.Second

	for {
		log.Info("Waiting for docker events")

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
				processEvent(ctx, cli, &event)
				if net != nil {
					err := processEventWithNet(ctx, cli, &event, net)
					if err != nil {
						log.Error(err)
					}
				}
			case err := <-errChan:
				log.WithFields(log.Fields{"retry": retry}).Error("Error reading docker events: ", err)
				quit = true
			}
		}

		cancel()
		time.Sleep(5 * time.Second)
	}
}

func processEvent(ctx context.Context, cli *client.Client, event *events.Message) {
	if event.Type == "container" {
		cid := event.Actor.ID
		if cid == "" {
			return
		}
		cname := event.Actor.Attributes["name"]
		image := event.Actor.Attributes["image"]
		if strings.HasPrefix(image, "sha256:") {
			// ignore temporary containers
			return
		}
		logger := log.WithFields(log.Fields{
			"event": event.Action,
			"cid":   cid[0:12],
			"cname": cname,
			"image": image,
		})
		if event.Action == "create" {
			logger.Info("Container created")
			return
		}
		if event.Action == "start" {
			logger.Info("Container started")
			return
		}
		if event.Action == "die" {
			exitCode, _ := strconv.Atoi(event.Actor.Attributes["exitCode"])
			if exitCode == 0 {
				logger.Info("Container exited normally")
			} else if exitCode > 128 {
				logger.
					WithFields(log.Fields{"signal": exitCode - 128}).
					Error("Container killed with signal")
			} else {
				logger.
					WithFields(log.Fields{"exitCode": exitCode}).
					Error("Container exited with error")
			}
			return
		}
		if event.Action == "destroy" {
			logger.Info("Container destroyed")
			err := updateImageChainExpireTime(ctx, cli, []string{image})
			if err != nil {
				logger.Error(err)
			}
			return
		}
		if strings.HasPrefix(event.Action, "exec_start: ") {
			logger.
				WithFields(log.Fields{"event": "exec", "cmd": strings.TrimSpace(event.Action[12:])}).
				Info("Container exec")
			return
		}
		if event.Action == "oom" {
			logger.Error("Container triggered OOM")
			return
		}
	}

	if event.Type == "image" {
		if event.Action == "pull" {
			log.WithFields(log.Fields{
				"event": "pull",
				"image": event.Actor.ID,
			}).Info("Docker image pulled")
			return
		}

		if event.Action == "delete" {
			removeImageExpireTime(event.Actor.ID)
			return
		}
	}
}

func processEventWithNet(ctx context.Context, cli *client.Client, event *events.Message, net *DockerNet) error {
	if event.Type != "container" {
		return nil
	}
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
	if !shouldExposeContainer(cname, image) {
		return nil
	}
	att := Attachment{
		cid:   cid,
		cname: cname,
		net:   net,
	}
	if event.Action == "create" {
		return attachContainerToNet(ctx, cli, &att)
	}
	if event.Action == "destroy" {
		return detachContainerFromNet(ctx, cli, &att)
	}
	if event.Action == "start" {
		if net.driver == "bridge" {
			return routePorts(ctx, cli, &att)
		}
		return nil
	}
	if event.Action == "die" {
		if net.driver == "bridge" {
			return unroutePorts(ctx, cli, &att)
		}
		return nil
	}

	return nil
}

func shouldExposeContainer(cname string, image string) bool {
	// temporarily exclude vast.ai containers
	return !strings.HasPrefix(cname, "C.")
}
