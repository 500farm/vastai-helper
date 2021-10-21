package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

func pruneContainers(ctx context.Context, cli *client.Client) bool {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("status", "created"),
			filters.Arg("status", "exited"),
			filters.Arg("status", "dead"),
		),
	})
	if err != nil {
		log.WithField("err", err).Error("Error listing containers")
		return false
	}
	found := false
	for _, container := range containers {
		cname := strings.TrimLeft(container.Names[0], "/")
		if !strings.HasPrefix(cname, "C.") { // skip vast.ai containers
			logger := log.WithFields(log.Fields{
				"cid":   container.ID[:12],
				"cname": cname,
			})
			info, err := cli.ContainerInspect(ctx, container.ID)
			if err != nil {
				logger.WithField("err", err).Error("Error inspecting container")
				continue
			}
			logger = logger.WithField("image", info.Config.Image)
			finishTs, err := time.Parse(time.RFC3339, info.State.FinishedAt)
			if err != nil {
				logger.Errorf("Invalid FinishedAt value: %v", info.State.FinishedAt)
				continue
			}
			age := time.Since(finishTs).Round(time.Second)
			logger = logger.WithField("age", age)
			if age > *pruneAge {
				found = true
				err := cli.ContainerRemove(ctx, info.ID, types.ContainerRemoveOptions{})
				if err != nil {
					logger.WithField("err", err).Error("Error removing container")
				} else {
					logger.Info("Pruned container")
				}
			}
		}
	}
	return found
}

func pruneImages(ctx context.Context, cli *client.Client, args filters.Args) bool {
	report, err := cli.ImagesPrune(ctx, args)
	if err != nil {
		log.WithField("err", err).Error("Error pruning images", err)
	} else if len(report.ImagesDeleted) > 0 {
		images := []string{}
		for _, im := range report.ImagesDeleted {
			if im.Untagged != "" {
				images = append(images, "untagged: "+im.Untagged)
			}
			if im.Deleted != "" {
				images = append(images, "deleted: "+im.Deleted)
			}
		}
		log.Info("Pruned images:\n" + strings.Join(images, "\n"))
		log.Infof("Space reclaimed: %s", formatSpace(report.SpaceReclaimed))
		return true
	}
	return false
}

func pruneBuildCache(ctx context.Context, cli *client.Client) bool {
	report, err := cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("until", (*pruneAge).String())),
	})
	if err != nil {
		log.WithField("err", err).Error("Error pruning build cache", err)
	} else if len(report.CachesDeleted) > 0 {
		log.Info("Pruned build caches:\n" + strings.Join(report.CachesDeleted, "\n"))
		log.Infof("Space reclaimed: %s", formatSpace(report.SpaceReclaimed))
		return true
	}
	return false
}

func dockerPruneLoop(ctx context.Context, cli *client.Client) {
	for {
		log.WithFields(log.Fields{
			"prune-age":           *pruneAge,
			"hub-image-prune-age": *hubImagePruneAge,
		}).Info("Doing auto-prune")
		ok1 := pruneContainers(ctx, cli)
		ok2 := pruneImages(ctx, cli, filters.NewArgs(
			filters.Arg("until", (*pruneAge).String()),
			filters.Arg("dangling", "true"),
		))
		ok3 := pruneImages(ctx, cli, filters.NewArgs(
			filters.Arg("until", (*hubImagePruneAge).String()),
			filters.Arg("dangling", "false"),
		))
		ok4 := pruneBuildCache(ctx, cli)
		if !ok1 && !ok2 && !ok3 && !ok4 {
			log.Info("Nothing to prune")
		}
		time.Sleep(time.Hour)
	}
}

func formatSpace(bytes uint64) string {
	return fmt.Sprintf("%.2f MiB", float64(bytes/1024/1024))
}
