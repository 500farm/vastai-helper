package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type PortRange struct {
	proto     string
	startPort int
	endPort   int
}

type Attachment struct {
	cid   string
	cname string
	net   DockerNet
	ip    net.IP
}

func attachContainerToNet(ctx context.Context, cli *client.Client, att Attachment) error {
	att.ip = randomIp(att.net.prefix)
	ipstr := att.ip.String()
	log.Printf("%s: attaching to network %s with IP %s", att.cname, att.net.name, ipstr)

	return cli.NetworkConnect(ctx, att.net.id, att.cid, &network.EndpointSettings{
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv6Address: ipstr,
		},
	})
}

func randomIp(prefix net.IPNet) net.IP {
	result := make([]byte, 16)
	rand.Read(result)
	for i := 0; i < 16; i++ {
		result[i] = (prefix.IP[i] & prefix.Mask[i]) | (result[i] &^ prefix.Mask[i])
	}
	return result
}

func routePorts(ctx context.Context, cli *client.Client, att Attachment) error {
	return routeOrUnroutePorts(ctx, cli, att, false)
}

func unroutePorts(ctx context.Context, cli *client.Client, att Attachment) error {
	return routeOrUnroutePorts(ctx, cli, att, true)
}

func routeOrUnroutePorts(ctx context.Context, cli *client.Client, att Attachment, unroute bool) error {
	ranges, str, err := portsToExpose(ctx, cli, &att)
	if err != nil {
		return err
	}
	if len(ranges) == 0 {
		return nil
	}
	if !unroute {
		log.Printf("%s: exposing ports: %s", att.cname, str)
	} else {
		log.Printf("%s: unexposing ports: %s", att.cname, str)
	}

	ipt, err := iptables.New(iptables.IPFamily(iptables.ProtocolIPv6), iptables.Timeout(1))
	if err != nil {
		return err
	}

	for _, r := range ranges {
		rule := iptablesRule(r, att.ip)
		log.Printf("    %s", strings.Join(rule, " "))
		var err error
		if !unroute {
			err = ipt.AppendUnique("filter", "FORWARD", rule...)
		} else {
			err = ipt.DeleteIfExists("filter", "FORWARD", rule...)
		}
		if err != nil {
			log.Printf("Ip6tables error: %v", err)
		}
	}
	return nil
}

func iptablesRule(r PortRange, ip net.IP) []string {
	return []string{
		"-d", ip.String(),
		"-p", r.proto,
		"--dport", fmt.Sprintf("%d:%d", r.startPort, r.endPort),
		"-j", "ACCEPT",
	}
}

func portsToExpose(ctx context.Context, cli *client.Client, att *Attachment) ([]PortRange, string, error) {
	ranges := []PortRange{}
	ctJson, err := cli.ContainerInspect(ctx, att.cid)
	if err != nil {
		return ranges, "", err
	}
	str := ""

	if att.ip == nil {
		for _, netJson := range ctJson.NetworkSettings.Networks {
			if netJson.NetworkID == att.net.id &&
				netJson.IPAMConfig != nil {
				att.ip = net.ParseIP(netJson.IPAMConfig.IPv6Address)
				break
			}
		}
	}

	for portSpec := range ctJson.Config.ExposedPorts {
		ranges = append(ranges, portSpecToRange(portSpec))
		if str != "" {
			str += " "
		}
		str += portSpec.Port()
	}

	if strings.HasSuffix(att.cname, "/ssh") && !rangesContainPort(ranges, "tcp", 22) {
		ranges = append(ranges, singlePort(22))
	}
	if strings.HasSuffix(att.cname, "/jupyter") && !rangesContainPort(ranges, "tcp", 8080) {
		ranges = append(ranges, singlePort(8080))
	}

	return ranges, str, nil
}

func portSpecToRange(portSpec nat.Port) PortRange {
	startPort, endPort, _ := portSpec.Range()
	return PortRange{
		proto:     portSpec.Proto(),
		startPort: startPort,
		endPort:   endPort,
	}
}

func singlePort(port int) PortRange {
	return PortRange{
		proto:     "tcp",
		startPort: port,
		endPort:   port,
	}
}

func rangesContainPort(ports []PortRange, proto string, port int) bool {
	for _, r := range ports {
		if proto == r.proto && port >= r.startPort && port <= r.endPort {
			return true
		}
	}
	return false
}
