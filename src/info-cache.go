package main

import (
	"net/http"

	log "github.com/sirupsen/logrus"
)

type InfoCache struct {
}

var infoCache InfoCache

func (c *InfoCache) load() error {
	return nil
}

func (c *InfoCache) json() []byte {
	return []byte("{}")
}

func (c *InfoCache) start() error {
	if err := c.load(); err != nil {
		return err
	}
	go startWebServer()
	return nil
}

func startWebServer() {
	http.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(infoCache.json())
	})
	logger := log.WithFields(log.Fields{"bind": *webServerBind})
	logger.Info("Starting web server")
	if err := http.ListenAndServe(*webServerBind, nil); err != nil {
		logger.Error(err)
	}
}
