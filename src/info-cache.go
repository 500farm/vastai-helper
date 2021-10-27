package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
)

type InstanceIps struct {
	V4, V6 net.IP
}

type InstanceInfo struct {
	id          string
	Name        string
	Image       string
	Command     string
	Ports       []nat.Port
	Labels      map[string]string
	CudaVersion string
	Gpus        []int
	Created     time.Time
	Started     time.Time
	// Size        int64
	InternalIps InstanceIps
	ExternalIps InstanceIps
}
type InfoCache struct {
	HostName  string
	Gpus      []string
	Instances []InstanceInfo
}

var infoCache InfoCache

func (c *InfoCache) load(ctx context.Context, cli *client.Client) error {
	c.HostName, _ = os.Hostname()

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return err
	}
	c.Instances = make([]InstanceInfo, 0, len(containers))
	for _, container := range containers {
		inst, err := c.getContainerInfo(ctx, cli, container.ID)
		if err != nil {
			return err
		}
		c.Instances = append(c.Instances, inst)
	}
	return nil
}

func (c *InfoCache) getContainerInfo(ctx context.Context, cli *client.Client, cid string) (InstanceInfo, error) {
	ctJson, err := cli.ContainerInspect(ctx, cid)
	if err != nil {
		return InstanceInfo{}, err
	}

	name := strings.TrimPrefix(ctJson.Name, "/")
	inst := InstanceInfo{
		Name:    name,
		Image:   ctJson.Config.Image,
		Command: ctJson.Path + " " + strings.Join(ctJson.Args, " "),
		Labels:  ctJson.Config.Labels,
		Ports:   []nat.Port{},
		Gpus:    []int{},
	}

	inst.Started, _ = time.Parse(time.RFC3339Nano, ctJson.State.StartedAt)
	inst.Created, _ = time.Parse(time.RFC3339Nano, ctJson.Created)

	for t := range ctJson.Config.ExposedPorts {
		inst.Ports = append(inst.Ports, t)
	}

	for k, network := range ctJson.NetworkSettings.Networks {
		if k == "bridge" {
			inst.InternalIps.V4 = net.ParseIP(network.IPAddress)
			inst.InternalIps.V6 = net.ParseIP(network.GlobalIPv6Address)
		} else if strings.HasPrefix(k, "vastai") {
			inst.ExternalIps.V4 = net.ParseIP(network.IPAddress)
			inst.ExternalIps.V6 = net.ParseIP(network.GlobalIPv6Address)
		}
	}

	for _, s := range ctJson.Config.Env {
		t := strings.Split(s, "=")
		if len(t) == 2 && t[1] != "" {
			if t[0] == "CUDA_VERSION" {
				inst.CudaVersion = t[1]
			} else if t[0] == "NVIDIA_VISIBLE_DEVICES" {
				for _, u := range strings.Split(t[1], ",") {
					v, _ := strconv.Atoi(u)
					inst.Gpus = append(inst.Gpus, v)
				}
			}
		}
	}

	return inst, nil
}

func (c *InfoCache) updateContainerInfo(ctx context.Context, cli *client.Client, cid string) error {
	newInst, err := c.getContainerInfo(ctx, cli, cid)
	if err != nil {
		return err
	}
	c.deleteContainerInfo(cid)
	c.Instances = append(c.Instances, newInst)
	return nil
}

func (c *InfoCache) deleteContainerInfo(cid string) {
	result := make([]InstanceInfo, 0, len(c.Instances))
	for _, inst := range c.Instances {
		if inst.id != cid {
			result = append(result, inst)
		}
	}
	c.Instances = result
}

func (c *InfoCache) json() []byte {
	j, err := json.MarshalIndent(*c, "", "    ")
	if err == nil {
		return j
	}
	return errorJson(err)
}

func errorJson(err error) []byte {
	var e struct {
		Error string
	}
	e.Error = err.Error()
	j, _ := json.Marshal(e)
	return j
}

func (c *InfoCache) start(ctx context.Context, cli *client.Client) error {
	if err := c.load(ctx, cli); err != nil {
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
