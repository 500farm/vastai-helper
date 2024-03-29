package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/docker/go-units"
	log "github.com/sirupsen/logrus"
)

type ContainerIps struct {
	V4, V6 net.IP
}

type ContainerInfo struct {
	Status      string // created / restarting / running / removing / paused / exited / dead
	Name        string
	Image       string
	Command     string
	Ports       []nat.Port
	Labels      map[string]string
	CudaVersion string
	Gpus        []int
	Created     time.Time
	Started     *time.Time
	Finished    *time.Time
	StorageSize *int64
	InternalIps ContainerIps
	ExternalIps ContainerIps

	id string // internal
}
type InfoCache struct {
	HostName   string
	NumGpus    int
	GpuStatus  []string // idle / mining / busy
	Containers []ContainerInfo

	ctx        context.Context
	cli        *client.Client
	cachedJson []byte
}

func newInfoCache(ctx context.Context, cli *client.Client) *InfoCache {
	hostName, _ := os.Hostname()
	return &InfoCache{
		HostName: hostName,
		NumGpus:  getNumGpus(),
		ctx:      ctx,
		cli:      cli,
	}
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

func (c *InfoCache) getContainerInfo(cid string) (ContainerInfo, error) {
	ctJson, err := c.cli.ContainerInspect(c.ctx, cid)
	if err != nil {
		return ContainerInfo{}, err
	}

	name := strings.TrimPrefix(ctJson.Name, "/")
	inst := ContainerInfo{
		id:      ctJson.ID,
		Name:    name,
		Image:   ctJson.Config.Image,
		Command: ctJson.Path + " " + strings.Join(ctJson.Args, " "),
		Labels:  ctJson.Config.Labels,
		Ports:   []nat.Port{},
		Gpus:    []int{},
		Status:  ctJson.State.Status,
	}
	if !shouldCacheContainerInfo(name, inst.Image) {
		return ContainerInfo{}, fmt.Errorf("container %s (%s) should not be cached", name, inst.Image)
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

	t3, err := units.FromHumanSize(ctJson.HostConfig.StorageOpt["size"])
	if err == nil && t3 > 0 {
		inst.StorageSize = &t3
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

func (c *InfoCache) updateContainerInfo(cids []string) error {
	for _, cid := range cids {
		newInst, err := c.getContainerInfo(cid)
		if err != nil {
			return err
		}
		c._deleteContainerInfo(cid)
		c.Containers = append(c.Containers, newInst)
	}
	c.afterUpdate()
	return nil
}

func (c *InfoCache) deleteContainerInfo(cid string) error {
	c._deleteContainerInfo(cid)
	c.afterUpdate()
	return nil
}

func (c *InfoCache) _deleteContainerInfo(cid string) {
	result := make([]ContainerInfo, 0, len(c.Containers))
	for _, inst := range c.Containers {
		if inst.id != cid {
			result = append(result, inst)
		}
	}
	c.Containers = result
}

func (c *InfoCache) afterUpdate() {
	// fill GpuStatus
	c.GpuStatus = make([]string, c.NumGpus)
	for i := 0; i < c.NumGpus; i++ {
		c.GpuStatus[i] = "idle"
	}
	for _, inst := range c.Containers {
		if inst.Status == "running" {
			for _, i := range inst.Gpus {
				if inst.isMiningImage() {
					c.GpuStatus[i] = "mining"
				} else {
					c.GpuStatus[i] = "busy"
				}
			}
		}
	}

	// sort: running first, newest first
	sort.Slice(c.Containers, func(i, j int) bool {
		st1 := c.Containers[i].statusOrder()
		st2 := c.Containers[j].statusOrder()
		if st1 < st2 {
			return true
		}
		if st1 > st2 {
			return false
		}
		return c.Containers[i].Created.After(c.Containers[j].Created)
	})

	// cache json
	c.cachedJson = c.generateJson()
}

func (c *InfoCache) json() []byte {
	return c.cachedJson
}

func (c *InfoCache) generateJson() []byte {
	// filter out mining containers
	exposed := []ContainerInfo{}
	for _, inst := range c.Containers {
		if inst.shouldExpose() {
			exposed = append(exposed, inst)
		}
	}
	t := *c
	t.Containers = exposed

	var err error
	result, err := json.MarshalIndent(t, "", "    ")
	if err != nil {
		log.Error(err)
		result = []byte("{}")
	}
	return result
}

func (c *ContainerInfo) isMiningImage() bool {
	return strings.HasPrefix(c.Image, "sergeycheperis/docker-ethminer") ||
		strings.HasPrefix(c.Image, "500farm/docker-ethminer")
}

func (c *ContainerInfo) shouldExpose() bool {
	return !c.isMiningImage()
}

func (c *ContainerInfo) statusOrder() int {
	if c.Status == "running" {
		return 0
	}
	if c.Status == "paused" || c.Status == "restarting" {
		return 1
	}
	if c.Status == "exited" {
		return 2
	}
	if c.Status == "created" {
		return 3
	}
	return 4
}
