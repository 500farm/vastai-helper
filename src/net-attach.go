package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"strings"

	log "github.com/sirupsen/logrus"

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
	net   *DockerNet
	ipv4  net.IP
	ipv6  net.IP
}

func attachContainerToNet(ctx context.Context, cli *client.Client, att *Attachment) error {
	att.ipv6 = randomIp(att.net.v6prefix)
	ipstr := att.ipv6.String()
	log.WithFields(att.logFields()).
		WithFields(log.Fields{"net": att.net.name, "ip": ipstr}).
		Info("Attaching container to network")

	// TODO also detach from default net?

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

func routePorts(ctx context.Context, cli *client.Client, att *Attachment) error {
	return routeOrUnroutePorts(ctx, cli, att, false)
}

func unroutePorts(ctx context.Context, cli *client.Client, att *Attachment) error {
	return routeOrUnroutePorts(ctx, cli, att, true)
}

func routeOrUnroutePorts(ctx context.Context, cli *client.Client, att *Attachment, unroute bool) error {
	ranges, err := portsToExpose(ctx, cli, att)
	if err != nil {
		return err
	}
	if len(ranges) == 0 {
		return nil
	}
	var text1, text2 string
	if !unroute {
		text1 = "Exposing ports"
		text2 = "Adding ip6tables rule"
	} else {
		text1 = "Unexposing ports"
		text2 = "Removing ip6tables rule"
	}
	logger1 := log.WithFields(att.logFields())
	logger1.
		WithFields(log.Fields{"ports": rangesToString(ranges)}).
		Info(text1)

	ipt, err := iptables.New(iptables.IPFamily(iptables.ProtocolIPv6), iptables.Timeout(1))
	if err != nil {
		return err
	}

	for _, r := range ranges {
		rule := r.iptablesRule(att.ipv6)
		logger2 := logger1.WithFields(log.Fields{"rule": strings.Join(rule, " ")})
		logger2.Info(text2)
		var err error
		if !unroute {
			err = ipt.AppendUnique("filter", "FORWARD", rule...)
		} else {
			err = ipt.DeleteIfExists("filter", "FORWARD", rule...)
		}
		if err != nil {
			logger1.Error(err)
		}
	}
	return nil

	// TODO policy=DROP
}

func portsToExpose(ctx context.Context, cli *client.Client, att *Attachment) ([]PortRange, error) {
	ranges := []PortRange{}
	ctJson, err := cli.ContainerInspect(ctx, att.cid)
	if err != nil {
		return ranges, err
	}

	if att.ipv6 == nil {
		for _, netJson := range ctJson.NetworkSettings.Networks {
			if netJson.NetworkID == att.net.id &&
				netJson.IPAMConfig != nil {
				att.ipv6 = net.ParseIP(netJson.IPAMConfig.IPv6Address)
				break
			}
		}
	}
	if att.ipv6 == nil {
		return ranges, nil
	}

	for portSpec := range ctJson.Config.ExposedPorts {
		ranges = append(ranges, portSpecToRange(portSpec))
	}

	if strings.HasSuffix(att.cname, "/ssh") && !rangesContainPort(ranges, "tcp", 22) {
		ranges = append(ranges, singlePort(22))
	}
	if strings.HasSuffix(att.cname, "/jupyter") && !rangesContainPort(ranges, "tcp", 8080) {
		ranges = append(ranges, singlePort(8080))
	}

	return ranges, nil
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

func (r *PortRange) iptablesRule(ip net.IP) []string {
	dport := ""
	if r.startPort == r.endPort {
		dport = fmt.Sprintf("%d", r.startPort)
	} else {
		dport = fmt.Sprintf("%d:%d", r.startPort, r.endPort)
	}
	return []string{
		"-d", ip.String(),
		"-p", r.proto,
		"--dport", dport,
		"-j", "ACCEPT",
	}
}

func (r *PortRange) String() string {
	if r.endPort == r.startPort {
		return fmt.Sprintf("%d/%s", r.startPort, r.proto)
	}
	return fmt.Sprintf("%d-%d/%s", r.startPort, r.endPort, r.proto)
}

func rangesContainPort(ports []PortRange, proto string, port int) bool {
	for _, r := range ports {
		if proto == r.proto && port >= r.startPort && port <= r.endPort {
			return true
		}
	}
	return false
}

func rangesToString(ranges []PortRange) string {
	str := ""
	for _, r := range ranges {
		if str != "" {
			str += " "
		}
		str += r.String()
	}
	return str
}

func (att *Attachment) logFields() log.Fields {
	cid := att.cid
	if len(cid) > 12 {
		cid = cid[0:12]
	}
	return log.Fields{"cid": cid, "cname": att.cname}
}
