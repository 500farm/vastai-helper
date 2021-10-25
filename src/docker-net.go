package main

import (
	"context"
	"net"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type DockerNet struct {
	id        string
	name      string
	driver    string
	v6prefix  net.IPNet
	v6gateway net.IP
	v4prefix  net.IPNet
	v4gateway net.IP
	ifname    string // for ipvlan only
}

func selectOrCreateDockerNet(ctx context.Context, cli *client.Client, netConf *NetConf) (DockerNet, error) {
	driver := "bridge"
	if netConf.mode == Ipvlan {
		driver = "ipvlan"
	}

	dockerNets, err := enumDockerNets(ctx, cli, driver)
	if err != nil {
		return DockerNet{}, err
	}

	for _, dockerNet := range dockerNets {
		if isNetSuitable(dockerNet, netConf) {
			log.WithFields(dockerNet.logFields()).Info("Using network")
			return dockerNet, nil
		}
	}

	return createDockerNet(ctx, cli, driver, netConf)
}

func enumDockerNets(ctx context.Context, cli *client.Client, driver string) ([]DockerNet, error) {
	log.Infof("Enumerating %s networks created by vastai-helper", driver)

	resp, err := cli.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return []DockerNet{}, err
	}

	result := []DockerNet{}
	for _, netJson := range resp {
		if netJson.Attachable && netJson.EnableIPv6 && netJson.Driver == driver && strings.HasPrefix(netJson.Name, "vastai") {
			dockerNet := DockerNet{
				id:     netJson.ID,
				name:   netJson.Name,
				driver: netJson.Driver,
				ifname: netJson.Options["parent"],
			}
			for _, ipamJson := range netJson.IPAM.Config {
				if ipamJson.Subnet != "" && ipamJson.Gateway != "" {
					_, cidr, err := net.ParseCIDR(ipamJson.Subnet)
					if err == nil {
						if strings.Contains(ipamJson.Subnet, ":") {
							dockerNet.v6prefix = *cidr
							dockerNet.v6gateway = net.ParseIP(ipamJson.Gateway)
						} else {
							dockerNet.v4prefix = *cidr
							dockerNet.v4gateway = net.ParseIP(ipamJson.Gateway)
						}
					}
				}
			}
			log.WithFields(dockerNet.logFields()).Info("Found network")
			result = append(result, dockerNet)
		}
	}

	if len(result) == 0 {
		log.Info("None found")
	}
	return result, nil
}

func isNetSuitable(net DockerNet, netConf *NetConf) bool {
	if netConf.mode == Bridge {
		return net.v6prefix.String() == netConf.v6.prefix.String() &&
			net.v6prefix.Contains(net.v6gateway)
	}

	// for mode=ipvlan
	return net.v6prefix.String() == netConf.v6.prefix.String() &&
		net.v6gateway.Equal(netConf.v6.gateway) &&
		net.v4prefix.String() == netConf.v4.prefix.String() &&
		net.v4gateway.Equal(netConf.v4.gateway) &&
		net.ifname == netConf.ifname
}

func createDockerNet(ctx context.Context, cli *client.Client, driver string, netConf *NetConf) (DockerNet, error) {
	log.Infof("Will create new %s network", driver)

	name := "vastai-net"
	i := 0
	for netExists(ctx, cli, name) {
		i++
		name = "vastai-net" + strconv.Itoa(i)
	}

	dockerNet := DockerNet{
		id:        "",
		name:      name,
		v6prefix:  netConf.v6.prefix,
		v6gateway: netConf.v6.gateway,
		v4prefix:  netConf.v4.prefix,
		v4gateway: netConf.v4.gateway,
		ifname:    netConf.ifname,
	}

	options := make(map[string]string)
	if driver == "ipvlan" {
		options["ipvlan_mode"] = "l2"
		options["parent"] = netConf.ifname
	}

	ipamConfigs := []network.IPAMConfig{}
	if dockerNet.v6prefix.IP != nil {
		ipamConfigs = append(ipamConfigs, network.IPAMConfig{
			Subnet:  dockerNet.v6prefix.String(),
			Gateway: dockerNet.v6gateway.String(),
		})
	}
	if dockerNet.v4prefix.IP != nil {
		ipamConfigs = append(ipamConfigs, network.IPAMConfig{
			Subnet:  dockerNet.v4prefix.String(),
			Gateway: dockerNet.v4gateway.String(),
		})
	}

	resp, err := cli.NetworkCreate(ctx, dockerNet.name, types.NetworkCreate{
		CheckDuplicate: true,
		Driver:         driver,
		EnableIPv6:     true,
		IPAM: &network.IPAM{
			Driver: "default",
			Config: ipamConfigs,
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
		"id":        id,
		"name":      net.name,
		"driver":    net.driver,
		"ifname":    net.ifname,
		"v6.prefix": net.v6prefix.String(),
		"v6.gw":     net.v6gateway.String(),
		"v4.prefix": net.v4prefix.String(),
		"v4.gw":     net.v4gateway.String(),
	}
}
