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

func pruneContainers(ctx context.Context, cli *client.Client) {
}

func pruneImages(ctx context.Context, cli *client.Client, args filters.Args) {
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
	}
}

func pruneBuildCache(ctx context.Context, cli *client.Client) {
	report, err := cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("until", (*pruneAge).String())),
	})
	if err != nil {
		log.Errorf("Error pruning build cache: %v", err)
	} else if len(report.CachesDeleted) > 0 {
		log.Infoln("Pruned build caches:\n" + strings.Join(report.CachesDeleted, "\n"))
		log.Infoln("Space reclaimed: %s", formatSpace(report.SpaceReclaimed))
	}
}

func dockerPruneLoop(ctx context.Context, cli *client.Client) {
	for {
		pruneContainers(ctx, cli)
		pruneImages(ctx, cli, filters.NewArgs(
			filters.Arg("until", (*pruneAge).String()),
			filters.Arg("dangling", "true"),
		))
		pruneImages(ctx, cli, filters.NewArgs(
			filters.Arg("until", (*hubImagePruneAge).String()),
			filters.Arg("dangling", "false"),
		))
		pruneBuildCache(ctx, cli)
		time.Sleep(time.Hour)
	}
}

func formatSpace(bytes uint64) string {
	return fmt.Sprintf("%.2f MiB", float64(bytes/1024/1024))
}
