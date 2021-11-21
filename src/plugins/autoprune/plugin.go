package autoprune

import (
	"context"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	expireTime = kingpin.Flag(
		"expire-time",
		"Expire time for stopped containers (non-VastAi), temporary images, build cache.",
	).Default("24h").Duration()
	taggedImageExpireTime = kingpin.Flag(
		"tagged-image-expire-time",
		"Prune age for tagged images.",
	).Default("168h").Duration()
	pruneInterval = kingpin.Flag(
		"prune-interval",
		"Interval between prune runs.",
	).Default("4h").Duration()
)

type AutoPrunePlugin struct {
	ctx    context.Context
	cli    *client.Client
	pruner *AutoPruner
}

func NewPlugin(ctx context.Context, cli *client.Client, stateDir string) *AutoPrunePlugin {
	return &AutoPrunePlugin{
		ctx:    ctx,
		cli:    cli,
		pruner: newAutoPruner(ctx, cli, stateDir+"prune/"),
	}
}

func (p *AutoPrunePlugin) Start() error {
	go p.pruner.loop()
	return nil
}

func (p *AutoPrunePlugin) ContainerCreated(cid string, cname string, image string) error {
	return nil
}

func (p *AutoPrunePlugin) ContainerDestroyed(cid string, cname string, image string) error {
	return p.pruner.updateImageChainExpireTime([]string{image})
}

func (p *AutoPrunePlugin) ContainerStarted(cid string, cname string, image string) error {
	return nil
}

func (p *AutoPrunePlugin) ContainerStopped(cid string, cname string, image string) error {
	return nil
}

func (p *AutoPrunePlugin) ImageRemoved(image string) error {
	p.pruner.removeImageExpireTime(image)
	return nil
}
