package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
	log "github.com/sirupsen/logrus"
)

type InstanceIps struct {
	V4, V6 net.IP `json:",omitempty"`
}

type InstanceInfo struct {
	Status      string
	Name        string
	Image       string
	Command     string
	Ports       []nat.Port
	Labels      map[string]string
	CudaVersion string `json:",omitempty"`
	Gpus        []int
	Created     time.Time
	Started     *time.Time `json:",omitempty"`
	Finished    *time.Time `json:",omitempty"`
	StorageSize int64      `json:",omitempty"`
	InternalIps InstanceIps
	ExternalIps InstanceIps
	// internal
	id string
}
type InfoCache struct {
	HostName  string
	NumGpus   int
	GpuStatus []string
	Instances []InstanceInfo
	// internal
	cachedJson []byte
}

var infoCache InfoCache

func (c *InfoCache) load(ctx context.Context, cli *client.Client) error {
	c.HostName, _ = os.Hostname()
	c.NumGpus = getNumGpus()
	return c.update(ctx, cli)
}

func (c *InfoCache) update(ctx context.Context, cli *client.Client) error {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return err
	}
	c.Instances = make([]InstanceInfo, 0, len(containers))
	for _, container := range containers {
		if !shouldExposeContainer(container.Names[0], container.Image) {
			continue
		}
		inst, err := getContainerInfo(ctx, cli, container.ID)
		if err != nil {
			log.Error(err)
			continue
		}
		c.Instances = append(c.Instances, inst)
	}
	c.afterUpdate()
	return nil
}

func getNumGpus() int {
	cmd := exec.Command("nvidia-smi", "-L")
	out, err := cmd.Output()
	if err != nil {
		log.Error(err)
		return 0
	}
	return strings.Count(string(out), "\n")
}

func getContainerInfo(ctx context.Context, cli *client.Client, cid string) (InstanceInfo, error) {
	ctJson, err := cli.ContainerInspect(ctx, cid)
	if err != nil {
		return InstanceInfo{}, err
	}

	name := strings.TrimPrefix(ctJson.Name, "/")
	inst := InstanceInfo{
		id:      ctJson.ID,
		Name:    name,
		Image:   ctJson.Config.Image,
		Command: ctJson.Path + " " + strings.Join(ctJson.Args, " "),
		Labels:  ctJson.Config.Labels,
		Ports:   []nat.Port{},
		Gpus:    []int{},
		Status:  ctJson.State.Status,
	}
	if !shouldExposeContainer(name, inst.Image) {
		return InstanceInfo{}, fmt.Errorf("Container %s (%s) should not be exposed via API", name, inst.Image)
	}

	inst.Created, _ = time.Parse(time.RFC3339Nano, ctJson.Created)
	t1, err := time.Parse(time.RFC3339Nano, ctJson.State.StartedAt)
	if err == nil && !t1.IsZero() {
		inst.Started = &t1
	}
	t2, err := time.Parse(time.RFC3339Nano, ctJson.State.FinishedAt)
	if err == nil && !t2.IsZero() {
		inst.Finished = &t2
	}

	inst.StorageSize, err = units.FromHumanSize(ctJson.HostConfig.StorageOpt["size"])
	if err != nil {
		inst.StorageSize = 0
	}

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
	sort.Ints(inst.Gpus)
	return inst, nil
}

func (c *InfoCache) updateContainerInfo(ctx context.Context, cli *client.Client, cid string) error {
	newInst, err := getContainerInfo(ctx, cli, cid)
	if err != nil {
		return err
	}
	c._deleteContainerInfo(cid)
	c.Instances = append(c.Instances, newInst)
	c.afterUpdate()
	return nil
}

func (c *InfoCache) deleteContainerInfo(cid string) {
	c._deleteContainerInfo(cid)
	c.afterUpdate()
}

func (c *InfoCache) _deleteContainerInfo(cid string) {
	result := make([]InstanceInfo, 0, len(c.Instances))
	for _, inst := range c.Instances {
		if inst.id != cid {
			result = append(result, inst)
		}
	}
	c.Instances = result
}

func (c *InfoCache) afterUpdate() {
	// fill GpuStatus
	c.GpuStatus = make([]string, c.NumGpus)
	for i := 0; i < c.NumGpus; i++ {
		c.GpuStatus[i] = "idle"
	}
	for _, inst := range c.Instances {
		if inst.Status == "running" {
			for _, i := range inst.Gpus {
				if isMiningImage(inst.Image) {
					c.GpuStatus[i] = "mining"
				} else {
					c.GpuStatus[i] = "busy"
				}
			}
		}
	}

	// sort: running first, newest first
	sort.Slice(c.Instances, func(i, j int) bool {
		st1 := c.Instances[i].statusOrder()
		st2 := c.Instances[j].statusOrder()
		if st1 < st2 {
			return true
		}
		if st1 > st2 {
			return false
		}
		return c.Instances[i].Created.After(c.Instances[j].Created)
	})

	// cache json
	var err error
	c.cachedJson, err = json.MarshalIndent(*c, "", "    ")
	if err != nil {
		log.Error(err)
		c.cachedJson = []byte("{}")
	}
}

func (c *InfoCache) json() []byte {
	return c.cachedJson
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

func isMiningImage(image string) bool {
	return strings.HasPrefix(image, "sergeycheperis/docker-ethminer")
}

func shouldExposeContainer(cname string, image string) bool {
	return (strings.HasPrefix(cname, "C.") || strings.HasPrefix(cname, "/C.")) &&
		!isMiningImage(image)
}

func (inst *InstanceInfo) statusOrder() int {
	if inst.Status == "running" {
		return 0
	}
	if inst.Status == "paused" || inst.Status == "restarting" {
		return 1
	}
	if inst.Status == "exited" {
		return 2
	}
	if inst.Status == "created" {
		return 3
	}
	return 4
}
