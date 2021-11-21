package plugin

import (
	"context"
	"net/http"

	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	webServerBind = kingpin.Flag(
		"web-server-bind",
		"Web server listen address and/or port.",
	).Default(":9014").String()
)

type ApiPlugin struct {
	ctx   context.Context
	cli   *client.Client
	cache InfoCache
}

func NewPlugin(ctx context.Context, cli *client.Client) *ApiPlugin {
	return &ApiPlugin{
		ctx: ctx,
		cli: cli,
	}
}

func (p *ApiPlugin) Start() error {
	if err := p.cache.load(p.ctx, p.cli); err != nil {
		return err
	}
	go func() {
		http.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(p.cache.json())
		})
		logger := log.WithFields(log.Fields{"bind": *webServerBind})
		logger.Info("Starting web server")
		if err := http.ListenAndServe(*webServerBind, nil); err != nil {
			logger.Error(err)
		}
	}()
	return nil
}

func (p *ApiPlugin) ContainerCreated(cid string, cname string, image string) error {
	return p.cache.updateContainerInfo(p.ctx, p.cli, cid)
}

func (p *ApiPlugin) ContainerDestroyed(cid string, cname string, image string) error {
	return p.cache.deleteContainerInfo(cid)
}

func (p *ApiPlugin) ContainerStarted(cid string, cname string, image string) error {
	return p.cache.updateContainerInfo(p.ctx, p.cli, cid)
}

func (p *ApiPlugin) ContainerStopped(cid string, cname string, image string) error {
	return p.cache.updateContainerInfo(p.ctx, p.cli, cid)
}

func (p *ApiPlugin) ImageRemoved(image string) error {
	return nil
}
