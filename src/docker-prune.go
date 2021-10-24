package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
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
			if age > *expireTime {
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
	// TODO print summary, space reclaimed
	return found
}

func pruneImages(ctx context.Context, cli *client.Client) bool {
	images, err := cli.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		log.WithField("err", err).Error("Error listing images")
		return false
	}
	found := false
	for _, image := range images {
		if len(image.RepoTags) > 0 {
			if isImageUsed(ctx, cli, image.ID) {
				updateImageExpireTime(image.ID)
			} else {
				expire := getImageExpireTime(image.ID)
				if expire.Before(time.Now()) {
					found = true
					logger := log.WithFields(log.Fields{
						"image":   imageIdDisplay(image.ID),
						"tags":    image.RepoTags,
						"expired": time.Since(expire),
						"size":    image.Size,
					})
					_, err := cli.ImageRemove(ctx, image.ID, types.ImageRemoveOptions{})
					if err != nil {
						logger.WithField("err", err).Error("Error removing image")
					} else {
						logger.Info("Pruned image")
					}
				}
			}
		}
	}
	// TODO print summary, space reclaimed
	return found
}

func pruneTempImages(ctx context.Context, cli *client.Client) bool {
	report, err := cli.ImagesPrune(ctx, filters.NewArgs(
		filters.Arg("until", (*expireTime).String()),
		filters.Arg("dangling", "true"),
	))
	if err != nil {
		log.WithField("err", err).Error("Error pruning temporary images", err)
	} else if len(report.ImagesDeleted) > 0 {
		count := 0
		tags := []string{}
		for _, item := range report.ImagesDeleted {
			if item.Deleted != "" {
				count++
			}
			if item.Untagged != "" {
				tags = append(tags, item.Untagged)
			}
		}
		log.WithFields(log.Fields{
			"count": count,
			"tags":  tags,
			"size":  formatSpace(report.SpaceReclaimed),
		}).Info("Pruned temporary images")
		return true
	}
	return false
}

func pruneBuildCache(ctx context.Context, cli *client.Client) bool {
	report, err := cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("until", (*expireTime).String())),
	})
	if err != nil {
		log.WithField("err", err).Error("Error pruning build cache", err)
	} else if len(report.CachesDeleted) > 0 {
		log.WithFields(log.Fields{
			"count": len(report.CachesDeleted),
			"size":  formatSpace(report.SpaceReclaimed),
		}).Info("Pruned build caches")
		return true
	}
	return false
}

func dockerPruneLoop(ctx context.Context, cli *client.Client) {
	for {
		log.WithFields(log.Fields{
			"expire-time":              *expireTime,
			"tagged-image-expire-time": *taggedImageExpireTime,
			"interval":                 *pruneInterval,
		}).Info("Doing auto-prune")
		ok1 := pruneContainers(ctx, cli)
		ok2 := pruneImages(ctx, cli)
		ok3 := pruneTempImages(ctx, cli)
		ok4 := pruneBuildCache(ctx, cli)
		if !ok1 && !ok2 && !ok3 && !ok4 {
			log.Info("Nothing to prune")
		}
		time.Sleep(*pruneInterval)
	}
}

func formatSpace(bytes uint64) string {
	return fmt.Sprintf("%.2f MiB", float64(bytes/1024/1024))
}

func updateImageExpireTime(id string) time.Time {
	t := time.Now().Add(*taggedImageExpireTime)
	ioutil.WriteFile(pruneStateDir()+"expire_"+id, []byte(t.Format(time.RFC3339)), 0600)
	return t
}

func getImageExpireTime(id string) time.Time {
	str, err := ioutil.ReadFile(pruneStateDir() + "expire_" + id)
	if err == nil {
		t, err := time.Parse(time.RFC3339, string(str))
		if err == nil {
			return t
		}
	}
	// if no time recorded, set to +vastAiExpireTime from now
	return updateImageExpireTime(id)
}

func removeImageExpireTime(id string) {
	os.Remove(pruneStateDir() + "expire_" + id)
}

func pruneStateDir() string {
	return "/var/lib/vastai-helper/prune/"
}

func updateImageChainExpireTime(ctx context.Context, cli *client.Client, id string) error {
	chain, err := getImageChain(ctx, cli, id)
	if err != nil {
		return err
	}
	for _, item := range chain {
		t := updateImageExpireTime(item.id)
		log.WithFields(log.Fields{
			"image":   imageIdDisplay(item.id),
			"tags":    item.tags,
			"expires": t.Format(time.RFC3339),
		}).Info("Setting image expire time")
	}
	return nil
}

type ImageChainItem struct {
	id   string
	tags []string
}

func getImageChain(ctx context.Context, cli *client.Client, id string) ([]ImageChainItem, error) {
	result := []ImageChainItem{}
	history, err := cli.ImageHistory(ctx, id)
	if err != nil {
		log.WithFields(log.Fields{
			"err":   err,
			"image": imageIdDisplay(id),
		}).Error("Error getting image history")
		return result, err
	}
	for _, item := range history {
		if item.ID != "" && item.ID != "<missing>" && len(item.Tags) > 0 {
			result = append(result, ImageChainItem{item.ID, item.Tags})
		}
	}
	return result, nil
}

func imageIdDisplay(id string) string {
	if id == "" {
		return ""
	}
	return strings.TrimPrefix(id, "sha256:")[:12]
}

func isImageUsed(ctx context.Context, cli *client.Client, id string) bool {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		All:    true,
		Latest: true,
		Filters: filters.NewArgs(
			filters.Arg("ancestor", id),
		),
	})
	if err != nil {
		log.WithField("err", err).Error("Error listing containers")
		return true
	}
	return len(containers) > 0
}