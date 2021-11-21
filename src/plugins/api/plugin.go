package api

import (
	"context"
	"net/http"
	"strings"

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
	ctx                  context.Context
	cli                  *client.Client
	cache                *InfoCache
	discoveredContainers []string
}

func NewPlugin(ctx context.Context, cli *client.Client) *ApiPlugin {
	return &ApiPlugin{
		ctx:   ctx,
		cli:   cli,
		cache: newInfoCache(ctx, cli),
	}
}

func (p *ApiPlugin) ContainerDiscovered(cid string, cname string, image string) error {
	if shouldCacheContainerInfo(cname, image) {
		p.discoveredContainers = append(p.discoveredContainers, cid)
	}
	return nil
}

func (p *ApiPlugin) Start() error {
	err := p.cache.updateContainerInfo(p.discoveredContainers)
	if err != nil {
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
	if shouldCacheContainerInfo(cname, image) {
		return p.cache.updateContainerInfo([]string{cid})
	}
	return nil
}

func (p *ApiPlugin) ContainerDestroyed(cid string, cname string, image string) error {
	if shouldCacheContainerInfo(cname, image) {
		return p.cache.deleteContainerInfo(cid)
	}
	return nil
}

func (p *ApiPlugin) ContainerStarted(cid string, cname string, image string) error {
	if shouldCacheContainerInfo(cname, image) {
		return p.cache.updateContainerInfo([]string{cid})
	}
	return nil
}

func (p *ApiPlugin) ContainerStopped(cid string, cname string, image string) error {
	if shouldCacheContainerInfo(cname, image) {
		return p.cache.updateContainerInfo([]string{cid})
	}
	return nil
}

func (p *ApiPlugin) ImageRemoved(image string) error {
	return nil
}

func shouldCacheContainerInfo(cname string, image string) bool {
	// vast.ai containers only
	return strings.HasPrefix(cname, "C.") || strings.HasPrefix(cname, "/C.")
}
