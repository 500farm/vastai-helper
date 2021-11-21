package autoprune

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

// FIXME global var is bad bad
var pruneStateDir string

func dockerPruneLoop(ctx context.Context, cli *client.Client) {
	time.Sleep(time.Minute)
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

	count := 0
	size := uint64(0)

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
			if age > *expireTime {
				err := cli.ContainerRemove(ctx, info.ID, types.ContainerRemoveOptions{})
				if err != nil {
					logger.WithField("err", err).Error("Error removing container")
				} else {
					count++
					size += uint64(container.SizeRw)
				}
			}
		}
	}

	if count > 0 {
		log.WithFields(log.Fields{
			"count": count,
			"size":  formatSpace(size),
		}).Info("Pruned containers")
		return true
	}
	return false
}

func pruneImages(ctx context.Context, cli *client.Client) bool {
	images, err := cli.ImageList(ctx, types.ImageListOptions{})
	if err != nil {
		log.WithField("err", err).Error("Error listing images")
		return false
	}

	count := 0
	size := uint64(0)
	imageIds := []string{}
	tags := []string{}
	update := []string{}

	for _, image := range images {
		if len(image.RepoTags) == 0 { // consider only tagged images
			continue
		}
		if isImageUsed(ctx, cli, image.ID) { // for used image, update expiration
			update = append(update, image.ID)
			continue
		}
		// unused and tagged image
		if isImageExpired(ctx, cli, image.ID) {
			_, err := cli.ImageRemove(ctx, image.ID, types.ImageRemoveOptions{})
			if err != nil {
				log.WithFields(log.Fields{
					"image": imageIdDisplay(image.ID),
					"tags":  image.RepoTags,
					"err":   err,
				}).Error("Error removing image")
			} else {
				count++
				size += uint64(image.Size)
				imageIds = append(imageIds, imageIdDisplay(image.ID))
				tags = append(tags, image.RepoTags...)
			}
		}
	}

	if count > 0 {
		log.WithFields(log.Fields{
			"count":  count,
			"images": imageIds,
			"tags":   tags,
			"size":   formatSpace(size),
		}).Info("Pruned tagged images")
		return true
	}
	if len(update) > 0 {
		updateImageChainExpireTime(ctx, cli, update)
	}
	return false
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
		imageIds := []string{}
		tags := []string{}
		for _, item := range report.ImagesDeleted {
			if item.Deleted != "" {
				count++
				imageIds = append(imageIds, imageIdDisplay(item.Deleted))
			}
			if item.Untagged != "" {
				tags = append(tags, item.Untagged)
			}
		}
		log.WithFields(log.Fields{
			"count":  count,
			"images": imageIds,
			"tags":   tags,
			"size":   formatSpace(report.SpaceReclaimed),
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

func formatSpace(bytes uint64) string {
	return fmt.Sprintf("%.2f MiB", float64(bytes/1024/1024))
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

func isImageExpired(ctx context.Context, cli *client.Client, id string) bool {
	str, err := ioutil.ReadFile(pruneStateDir + "expire_" + id)
	if err == nil {
		expire, err := time.Parse(time.RFC3339, string(str))
		if err == nil {
			return expire.Before(time.Now())
		}
	}
	// if no time recorded, initialize it
	updateImageChainExpireTime(ctx, cli, []string{id})
	return false
}

func updateImageChainExpireTime(ctx context.Context, cli *client.Client, leafIds []string) error {
	imageIds := []string{}
	tags := []string{}
	for _, leafId := range unique(leafIds) {
		chain, err := getImageChain(ctx, cli, leafId)
		if err != nil {
			continue
		}
		for _, item := range chain {
			updateImageExpireTime(item.id)
			imageIds = append(imageIds, imageIdDisplay(item.id))
			tags = append(tags, item.tags...)
		}
	}
	log.WithFields(log.Fields{
		"images": unique(imageIds),
		"tags":   unique(tags),
		"expire": (time.Now().Add(*taggedImageExpireTime)).Format(time.RFC3339),
	}).Info("Updated image expiration")
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

func updateImageExpireTime(id string) {
	t := time.Now().Add(*taggedImageExpireTime)
	ioutil.WriteFile(pruneStateDir+"expire_"+id, []byte(t.Format(time.RFC3339)), 0600)
}

func removeImageExpireTime(id string) {
	os.Remove(pruneStateDir + "expire_" + id)
}
