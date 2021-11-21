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

func dockerEventLoop(ctx context.Context, cli *client.Client) {
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
			case err := <-errChan:
				log.WithFields(log.Fields{"retry": retry}).Error("Error reading docker events: ", err)
				quit = true
			}
		}

		cancel()
		time.Sleep(5 * time.Second)

		// TODO reload InfoCache because events could have been missed
		// TODO or better use the --since filter?
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
			// plugin call
			callPlugin(func(p Plugin) error {
				return p.ContainerCreated(cid, cname, image)
			}, logger)

		} else if event.Action == "start" {
			logger.Info("Container started")
			// plugin call
			callPlugin(func(p Plugin) error {
				return p.ContainerStarted(cid, cname, image)
			}, logger)

		} else if event.Action == "die" {
			exitCode, _ := strconv.Atoi(event.Actor.Attributes["exitCode"])
			if exitCode == 0 {
				logger.Info("Container exited normally")
			} else if exitCode > 128 {
				logger.
					WithFields(log.Fields{"signal": exitCode - 128}).
					Warn("Container killed with signal")
			} else {
				logger.
					WithFields(log.Fields{"exitCode": exitCode}).
					Warn("Container exited with error")
			}
			// plugin call
			callPlugin(func(p Plugin) error {
				return p.ContainerStopped(cid, cname, image)
			}, logger)

		} else if event.Action == "destroy" {
			logger.Info("Container destroyed")
			// plugin call
			callPlugin(func(p Plugin) error {
				return p.ContainerDestroyed(cid, cname, image)
			}, logger)

		} else if strings.HasPrefix(event.Action, "exec_start: ") {
			logger.
				WithFields(log.Fields{"event": "exec", "cmd": strings.TrimSpace(event.Action[12:])}).
				Info("Container exec")

		} else if event.Action == "oom" {
			logger.Warn("Container triggered OOM")
		}
	}

	if event.Type == "image" {
		logger := log.WithFields(log.Fields{
			"event": event.Action,
			"image": event.Actor.ID,
		})

		if event.Action == "pull" {
			logger.Info("Docker image pulled")

		} else if event.Action == "delete" {
			// plugin call
			callPlugin(func(p Plugin) error {
				return p.ImageRemoved(event.Actor.ID)
			}, logger)
		}
	}
}

func callPlugin(f func(p Plugin) error, logger *log.Entry) {
	for _, p := range plugins {
		err := f(p)
		if err != nil {
			logger.Error(err)
		}
	}
}
