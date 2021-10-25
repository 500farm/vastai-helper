package main

import (
	"context"
	"net"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type DockerNet struct {
	id      string
	name    string
	driver  string
	prefix  net.IPNet
	gateway net.IP
}

func selectOrCreateDockerNet(ctx context.Context, cli *client.Client, driver string, netConf *NetConf) (DockerNet, error) {
	dockerNets, err := enumDockerNets(ctx, cli, driver)
	if err != nil {
		return DockerNet{}, err
	}

	for _, dockerNet := range dockerNets {
		if dockerNet.prefix.String() == netConf.prefix.String() &&
			dockerNet.prefix.Contains(dockerNet.gateway) {
			log.WithFields(dockerNet.logFields()).Info("Using network")
			return dockerNet, nil
		}
	}

	return createDockerNet(ctx, cli, driver, netConf)
}

func enumDockerNets(ctx context.Context, cli *client.Client, driver string) ([]DockerNet, error) {
	log.Infof("Enumerating IPv6-enabled user-defined %s networks", driver)

	resp, err := cli.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return []DockerNet{}, err
	}

	result := []DockerNet{}
	for _, netJson := range resp {
		if netJson.Attachable && netJson.EnableIPv6 && netJson.Driver == driver {
			dockerNet := DockerNet{
				id:     netJson.ID,
				name:   netJson.Name,
				driver: netJson.Driver,
			}
			for _, ipamJson := range netJson.IPAM.Config {
				if strings.Contains(ipamJson.Subnet, ":") &&
					strings.Contains(ipamJson.Gateway, ":") {
					_, cidr, err := net.ParseCIDR(ipamJson.Subnet)
					if err == nil {
						dockerNet.prefix = *cidr
						dockerNet.gateway = net.ParseIP(ipamJson.Gateway)
						log.WithFields(dockerNet.logFields()).Info("Found network")
						result = append(result, dockerNet)
						break
					}
				}
			}
		}
	}

	if len(result) == 0 {
		log.Info("None found")
	}
	return result, nil
}

func createDockerNet(ctx context.Context, cli *client.Client, driver string, netConf *NetConf) (DockerNet, error) {
	log.Infof("Will create new %s network", driver)

	name := "vastai-ipv6-net"
	i := 0
	for netExists(ctx, cli, name) {
		i++
		name = "vastai-ipv6-net" + strconv.Itoa(i)
	}

	dockerNet := DockerNet{
		id:      "",
		name:    name,
		prefix:  netConf.prefix,
		gateway: gwAddress(netConf.prefix),
	}

	options := make(map[string]string)
	if driver == "ipvlan" {
		options["ipvlan_mode"] = "l3"
	}

	resp, err := cli.NetworkCreate(ctx, dockerNet.name, types.NetworkCreate{
		CheckDuplicate: true,
		Driver:         driver,
		EnableIPv6:     true,
		IPAM: &network.IPAM{
			Driver: "default",
			Config: []network.IPAMConfig{{
				Subnet:  dockerNet.prefix.String(),
				Gateway: dockerNet.gateway.String(),
			}},
		},
		Attachable: true,
		Options:    options,
	})
	if err != nil {
		return DockerNet{}, err
	}

	dockerNet.id = resp.ID
	log.WithFields(dockerNet.logFields()).Info("Network created")
	return dockerNet, nil
}

func gwAddress(prefix net.IPNet) net.IP {
	result, _ := cidr.Host(&prefix, 1)
	return result
}

func netExists(ctx context.Context, cli *client.Client, name string) bool {
	networks, err := cli.NetworkList(ctx, types.NetworkListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	return err == nil && len(networks) > 0
}

func (net *DockerNet) logFields() log.Fields {
	id := net.id
	if len(id) > 12 {
		id = id[0:12]
	}
	return log.Fields{
		"id":     id,
		"name":   net.name,
		"driver": net.driver,
		"prefix": net.prefix.String(),
		"gw":     net.gateway.String(),
	}
}
