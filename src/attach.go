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

func attachContainerToNet(ctx context.Context, cli *client.Client, cid string, cname string, net DockerNet) error {
	ip := randomIp(net.prefix).String()
	log.Printf("%s: attaching to network %s with IP %s", cname, net.name, ip)
	err := cli.NetworkConnect(ctx, net.id, cid, &network.EndpointSettings{
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv6Address: ip,
		},
	})
	if err != nil {
		return err
	}
	return routePorts(ctx, cli, cid)
}

func cleanupContainer(ctx context.Context, cli *client.Client, cid string) error {
	return unroutePorts(ctx, cli, cid)
}

func randomIp(prefix net.IPNet) net.IP {
	result := make([]byte, 16)
	rand.Read(result)
	for i := 0; i < 16; i++ {
		result[i] = (prefix.IP[i] & prefix.Mask[i]) | (result[i] &^ prefix.Mask[i])
	}
	return result
}

func routePorts(ctx context.Context, cli *client.Client, cid string) error {
	return routeOrUnroutePorts(ctx, cli, cid, false)
}

func unroutePorts(ctx context.Context, cli *client.Client, cid string) error {
	return routeOrUnroutePorts(ctx, cli, cid, true)
}

func routeOrUnroutePorts(ctx context.Context, cli *client.Client, cid string, unroute bool) error {
	rules, str, err := portsToExpose(ctx, cli, cid)
	if err != nil {
		return err
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

func portsToExpose(ctx context.Context, cli *client.Client, cid string) ([]DockerRule, string, error) {
	rules := []DockerRule{}
	ctJson, err := cli.ContainerInspect(ctx, cid)
	if err != nil {
		return rules, "", err
	}
	str := ""
	ip := net.IP{}
	for netName, netJson := range ctJson.NetworkSettings.Networks {
		if strings.HasPrefix(netName, "vastai-") {
			ipstr := netJson.GlobalIPv6Address
			gwstr := netJson.IPv6Gateway
			if ipstr == "" || gwstr == "" {
				return rules, str, fmt.Errorf("No IP or gateway in config of net %s", netName)
			}
			ip = net.ParseIP(ipstr)
			break
		}
	}
	for portSpec := range ctJson.NetworkSettings.Ports {
		startPort, endPort, _ := portSpec.Range()
		rules = append(rules, DockerRule{
			proto:     portSpec.Proto(),
			startPort: startPort,
			endPort:   endPort,
			ip:        ip,
		})
		if str != "" {
			str += " "
		}
		str += portSpec.Port()
	}
	str += " -> " + ip.String()
	return rules, str, nil
}

func ruleSpec(rule DockerRule) []string {
	return []string{
		"-d", rule.ip.String(),
		"-p", rule.proto,
		"--dport", fmt.Sprintf("%d:%d", rule.startPort, rule.endPort),
	}
}
