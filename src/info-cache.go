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
	id      string
	running bool
}
type InfoCache struct {
	HostName  string
	NumGpus   int
	GpuStatus []string
	Instances []InstanceInfo
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
		if !shouldExposeContainer(container.Names[0]) {
			continue
		}
		inst, err := getContainerInfo(ctx, cli, container.ID)
		if err != nil {
			return err
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
	if !shouldExposeContainer(name) {
		return InstanceInfo{}, fmt.Errorf("Container %s should not be exposed via API", name)
	}
	inst := InstanceInfo{
		Name:    name,
		Image:   ctJson.Config.Image,
		Command: ctJson.Path + " " + strings.Join(ctJson.Args, " "),
		Labels:  ctJson.Config.Labels,
		Ports:   []nat.Port{},
		Gpus:    []int{},
		Status:  ctJson.State.Status,
	}

	inst.Created, _ = time.Parse(time.RFC3339Nano, ctJson.Created)
	t, err := time.Parse(time.RFC3339Nano, ctJson.State.StartedAt)
	if err == nil && !t.IsZero() {
		inst.Started = &t
	}
	t, err = time.Parse(time.RFC3339Nano, ctJson.State.FinishedAt)
	if err == nil && !t.IsZero() {
		inst.Finished = &t
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

	inst.running = inst.Status == "running"
	return inst, nil
}

func (c *InfoCache) updateContainerInfo(ctx context.Context, cli *client.Client, cid string) error {
	newInst, err := getContainerInfo(ctx, cli, cid)
	if err != nil {
		return err
	}
	c.deleteContainerInfo(cid)
	c.Instances = append(c.Instances, newInst)
	c.afterUpdate()
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
	c.afterUpdate()
}

func (c *InfoCache) afterUpdate() {
	// fill GpuStatus
	c.GpuStatus = make([]string, c.NumGpus)
	for i := 0; i < c.NumGpus; i++ {
		c.GpuStatus[i] = "idle"
	}
	for _, inst := range c.Instances {
		if inst.running {
			for _, i := range inst.Gpus {
				if strings.HasPrefix(inst.Image, "sergeycheperis/docker-ethminer") {
					c.GpuStatus[i] = "mining"
				} else {
					c.GpuStatus[i] = "busy"
				}
			}
		}
	}

	// sort: running first, newest first
	sort.Slice(c.Instances, func(i, j int) bool {
		running1 := c.Instances[i].running
		running2 := c.Instances[j].running
		if running1 && !running2 {
			return true
		} else if !running1 && running2 {
			return false
		} else {
			return c.Instances[i].Created.After(c.Instances[j].Created)
		}
	})
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

func shouldExposeContainer(cname string) bool {
	return strings.HasPrefix(cname, "C.") || strings.HasPrefix(cname, "/C.")
}
