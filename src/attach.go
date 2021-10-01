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

type DockerRule struct {
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
	rules, str, err := portsToExpose(ctx, cli, &att)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
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

	for _, rule := range rules {
		spec := ruleSpec(rule, att.ip)
		log.Printf("    %s", strings.Join(spec, " "))
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

func ruleSpec(rule DockerRule, ip net.IP) []string {
	return []string{
		"-d", ip.String(),
		"-p", rule.proto,
		"--dport", fmt.Sprintf("%d:%d", rule.startPort, rule.endPort),
		"-j", "ACCEPT",
	}
}

func portsToExpose(ctx context.Context, cli *client.Client, att *Attachment) ([]DockerRule, string, error) {
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
		rules = append(rules, portSpecToRule(portSpec))
		if str != "" {
			str += " "
		}
		str += portSpec.Port()
	}

	if strings.HasSuffix(att.cname, "/ssh") && !rulesContainPort(rules, "tcp", 22) {
		rules = append(rules, tcpRule(22))
	}
	if strings.HasSuffix(att.cname, "/jupyter") && !rulesContainPort(rules, "tcp", 8080) {
		rules = append(rules, tcpRule(8080))
	}

	return rules, str, nil
}

func portSpecToRule(portSpec nat.Port) DockerRule {
	startPort, endPort, _ := portSpec.Range()
	return DockerRule{
		proto:     portSpec.Proto(),
		startPort: startPort,
		endPort:   endPort,
	}
}

func tcpRule(port int) DockerRule {
	return DockerRule{
		proto:     "tcp",
		startPort: port,
		endPort:   port,
	}
}

func rulesContainPort(rules []DockerRule, proto string, port int) bool {
	for _, rule := range rules {
		if proto == rule.proto && port >= rule.startPort && port <= rule.endPort {
			return true
		}
	}
	return false
}
