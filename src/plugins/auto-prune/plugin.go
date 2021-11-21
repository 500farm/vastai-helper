package plugin

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
	ctx context.Context
	cli *client.Client
}

func NewPlugin(ctx context.Context, cli *client.Client) *AutoPrunePlugin {
	return &AutoPrunePlugin{
		ctx: ctx,
		cli: cli,
	}
}

func (p *AutoPrunePlugin) Start() error {
	go dockerPruneLoop(p.ctx, p.cli)
	return nil
}

func (p *AutoPrunePlugin) ContainerCreated(cid string, cname string, image string) error {
	return nil
}

func (p *AutoPrunePlugin) ContainerDestroyed(cid string, cname string, image string) error {
	return updateImageChainExpireTime(p.ctx, p.cli, []string{image})
}

func (p *AutoPrunePlugin) ContainerStarted(cid string, cname string, image string) error {
	return nil
}

func (p *AutoPrunePlugin) ContainerStopped(cid string, cname string, image string) error {
	return nil
}

func (p *AutoPrunePlugin) ImageRemoved(image string) error {
	removeImageExpireTime(image)
	return nil
}
