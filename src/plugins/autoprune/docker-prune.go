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

type AutoPruner struct {
	ctx      context.Context
	cli      *client.Client
	stateDir string
}

func newAutoPruner(ctx context.Context, cli *client.Client, stateDir string) *AutoPruner {
	return &AutoPruner{
		ctx:      ctx,
		cli:      cli,
		stateDir: stateDir,
	}
}

func (p *AutoPruner) loop() {
	os.MkdirAll(p.stateDir, 0700)
	time.Sleep(time.Minute)
	for {
		log.WithFields(log.Fields{
			"expire-time":              *expireTime,
			"tagged-image-expire-time": *taggedImageExpireTime,
			"interval":                 *pruneInterval,
		}).Info("Doing auto-prune")
		ok1 := p.pruneContainers()
		ok2 := p.pruneImages()
		ok3 := p.pruneTempImages()
		ok4 := p.pruneBuildCache()
		if !ok1 && !ok2 && !ok3 && !ok4 {
			log.Info("Nothing to prune")
		}
		time.Sleep(*pruneInterval)
	}
}

func (p *AutoPruner) pruneContainers() bool {
	containers, err := p.cli.ContainerList(p.ctx, types.ContainerListOptions{
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
			info, err := p.cli.ContainerInspect(p.ctx, container.ID)
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
				err := p.cli.ContainerRemove(p.ctx, info.ID, types.ContainerRemoveOptions{})
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

func (p *AutoPruner) pruneImages() bool {
	images, err := p.cli.ImageList(p.ctx, types.ImageListOptions{})
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
		if p.isImageUsed(image.ID) { // for used image, update expiration
			update = append(update, image.ID)
			continue
		}
		// unused and tagged image
		if p.isImageExpired(image.ID) {
			_, err := p.cli.ImageRemove(p.ctx, image.ID, types.ImageRemoveOptions{})
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
		p.updateImageChainExpireTime(update)
	}
	return false
}

func (p *AutoPruner) pruneTempImages() bool {
	report, err := p.cli.ImagesPrune(p.ctx, filters.NewArgs(
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

func (p *AutoPruner) pruneBuildCache() bool {
	report, err := p.cli.BuildCachePrune(p.ctx, types.BuildCachePruneOptions{
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

func (p *AutoPruner) isImageUsed(id string) bool {
	containers, err := p.cli.ContainerList(p.ctx, types.ContainerListOptions{
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

func (p *AutoPruner) isImageExpired(id string) bool {
	str, err := ioutil.ReadFile(p.stateDir + "expire_" + id)
	if err == nil {
		expire, err := time.Parse(time.RFC3339, string(str))
		if err == nil {
			return expire.Before(time.Now())
		}
	}
	// if no time recorded, initialize it
	p.updateImageChainExpireTime([]string{id})
	return false
}

func (p *AutoPruner) updateImageChainExpireTime(leafIds []string) error {
	imageIds := []string{}
	tags := []string{}
	for _, leafId := range unique(leafIds) {
		chain, err := p.getImageChain(leafId)
		if err != nil {
			continue
		}
		for _, item := range chain {
			p.updateImageExpireTime(item.id)
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

func (p *AutoPruner) getImageChain(id string) ([]ImageChainItem, error) {
	result := []ImageChainItem{}
	history, err := p.cli.ImageHistory(p.ctx, id)
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

func (p *AutoPruner) updateImageExpireTime(id string) {
	t := time.Now().Add(*taggedImageExpireTime)
	ioutil.WriteFile(p.stateDir+"expire_"+id, []byte(t.Format(time.RFC3339)), 0600)
}

func (p *AutoPruner) removeImageExpireTime(id string) {
	os.Remove(p.stateDir + "expire_" + id)
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
