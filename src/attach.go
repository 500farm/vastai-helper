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
)

type DockerRule struct {
	proto     string
	startPort int
	endPort   int
	ip        net.IP
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

	err := cli.NetworkConnect(ctx, att.net.id, att.cid, &network.EndpointSettings{
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv6Address: ipstr,
		},
	})
	if err != nil {
		return err
	}

	return routePorts(ctx, cli, att)
}

func cleanupContainer(ctx context.Context, cli *client.Client, att Attachment) error {
	return unroutePorts(ctx, cli, att)
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
	rules, str, err := portsToExpose(ctx, cli, att)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	if !unroute {
		log.Printf("Exposing ports: %s", str)
	} else {
		log.Printf("Unexposing ports: %s", str)
	}

	ipt, err := iptables.New(iptables.IPFamily(iptables.ProtocolIPv6), iptables.Timeout(1))
	if err != nil {
		return err
	}

	for _, rule := range rules {
		spec := ruleSpec(rule)
		log.Printf("%s", strings.Join(spec, " "))
		var err error
		if !unroute {
			err = ipt.AppendUnique("filter", "FORWARD", spec...)
		} else {
			err = ipt.DeleteIfExists("filter", "FORWARD", spec...)
		}
		if err != nil {
			log.Printf("Ip6tables error: %v", err)
		}
	}
	return nil
}

func portsToExpose(ctx context.Context, cli *client.Client, att Attachment) ([]DockerRule, string, error) {
	rules := []DockerRule{}
	ctJson, err := cli.ContainerInspect(ctx, att.cid)
	if err != nil {
		return rules, "", err
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
		startPort, endPort, _ := portSpec.Range()
		rules = append(rules, DockerRule{
			proto:     portSpec.Proto(),
			startPort: startPort,
			endPort:   endPort,
			ip:        att.ip,
		})
		if str != "" {
			str += " "
		}
		str += portSpec.Port()
	}

	return rules, str, nil
}

func ruleSpec(rule DockerRule) []string {
	return []string{
		"-d", rule.ip.String(),
		"-p", rule.proto,
		"--dport", fmt.Sprintf("%d:%d", rule.startPort, rule.endPort),
	}
}
