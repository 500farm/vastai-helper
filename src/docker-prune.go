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
		log.Errorf("Error listing containers: %v", err)
		return false
	}
	found := false
	for _, container := range containers {
		if !strings.HasPrefix(container.Names[0], "C.") { // skip vast.ai containers
			info, err := cli.ContainerInspect(ctx, container.ID)
			if err != nil {
				log.Errorf("Error inspecting container %s: %v", container.ID[:12], err)
				continue
			}
			finishTs, err := time.Parse(time.RFC3339, info.State.FinishedAt)
			if err != nil {
				log.Errorf("Container %s has invalid FinishedAt value %v", info.ID[:12], info.State.FinishedAt)
				continue
			}
			if time.Since(finishTs) > *pruneAge {
				found = true
				err := cli.ContainerRemove(ctx, info.ID, types.ContainerRemoveOptions{})
				if err != nil {
					log.Errorf("Error removing container %s: %v", container.ID[:12], err)
				}
			}
		}
	}
	return found
}

func pruneImages(ctx context.Context, cli *client.Client, args filters.Args) bool {
	report, err := cli.ImagesPrune(ctx, args)
	if err != nil {
		log.Errorf("Error pruning images: %v", err)
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
		log.Infoln("Pruned images:\n" + strings.Join(images, "\n"))
		log.Infoln("Space reclaimed: %s", formatSpace(report.SpaceReclaimed))
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
		log.Errorf("Error pruning build cache: %v", err)
	} else if len(report.CachesDeleted) > 0 {
		log.Infoln("Pruned build caches:\n" + strings.Join(report.CachesDeleted, "\n"))
		log.Infoln("Space reclaimed: %s", formatSpace(report.SpaceReclaimed))
		return true
	}
	return false
}

func dockerPruneLoop(ctx context.Context, cli *client.Client) {
	for {
		log.Infoln("Doing auto-prune")
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
			log.Infoln("Nothing to prune")
		}
		time.Sleep(time.Hour)
	}
}

func formatSpace(bytes uint64) string {
	return fmt.Sprintf("%.2f MiB", float64(bytes/1024/1024))
}
