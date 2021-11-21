package plugin

import (
	"context"
	"os"
	"strings"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	// network attach functionality
	netTypeArg = kingpin.Flag(
		"net-type",
		"Network type: 'bridge' or 'ipvlan'.",
	).String()
	netInterface = kingpin.Flag(
		"net-interface",
		"Network interface for DHCPv4 and DHCPv6-PD queries.",
	).String()
	ipv6Prefix = kingpin.Flag(
		"ipv6-prefix",
		"Static IPv6 prefix for address assignment (length from /48 to /96).",
	).String()
	ipv6Gateway = kingpin.Flag(
		"ipv6-gateway",
		"Static IPv6 gateway address (must be inside --ipv6-prefix).",
	).String()

	// testing
	test = kingpin.Flag(
		"test",
		"Perform a self-test of network attach functionality of the running daemon.",
	).Bool()
	debug = kingpin.Flag(
		"debug",
		"Print DHCP packets.",
	).Bool()
)

type NetAttachPlugin struct {
	ctx     context.Context
	cli     *client.Client
	enabled bool
	net     *DockerNet
}

func NewPlugin(ctx context.Context, cli *client.Client) *NetAttachPlugin {
	return &NetAttachPlugin{
		ctx: ctx,
		cli: cli,
	}
}

func (p *NetAttachPlugin) Start() error {
	if *test {
		// self-test mode
		if err := selfTest(ctx, cli); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}

	var netType NetType
	switch *netTypeArg {
	case "":
		netType = None
	case "bridge":
		netType = Bridge
	case "ipvlan":
		netType = Ipvlan
	default:
		log.Fatalf(`Invalid --net-type="%s".`, netTypeArg)
	}

	if netType != None && *netInterface == "" {
		log.Fatal("Network functionality requires --net-interface.")
	}

	if netType != None {
		var netConfV6, netConfV4 NetConf
		var err error

		if netType == Bridge {
			if *ipv6Prefix != "" {
				netConfV6, err = staticNetConfV6(*ipv6Prefix, "")
			} else {
				netConfV6, err = dhcpNetConfV6(ctx, *netInterface, false)
			}
			if err != nil {
				log.Fatal(err)
			}

		} else if netType == Ipvlan {
			if *ipv6Prefix != "" {
				netConfV6, err = staticNetConfV6(*ipv6Prefix, *ipv6Gateway)
			} else {
				// TODO auto configuration via NDP
				log.Fatal("IPv6 auto-configuration is not supported in Ipvlan mode, please use --ipv6-prefix.")
			}
			if err != nil {
				log.Fatal(err)
			}
			netConfV4, err = dhcpNetConfV4(ctx, *netInterface)
			if err != nil {
				log.Fatal(err)
			}
			go dhcpRenewLoopV4(ctx)
		}

		netConf := mergeNetConf(netConfV4, netConfV6, netType)
		log.WithFields(netConf.logFields()).Info("Detected network configuration")

		p.net, err = selectOrCreateDockerNet(ctx, cli, &netConf)
		if err != nil {
			log.Fatal(err)
		}

		p.enabled = true
	}
}

func (p *NetAttachPlugin) ContainerCreated(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) {
		return attachContainerToNet(ctx, cli, &Attachment{cid: cid, cname: cname, net: p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ContainerDestroyed(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) {
		return detachContainerFromNet(ctx, cli, &Attachment{cid: cid, cname: cname, net: p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ContainerStarted(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) &&
		p.net.driver == "bridge" {
		return routePorts(ctx, cli, &Attachment{cid: cid, cname: cname, net: p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ContainerStopped(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) &&
		p.net.driver == "bridge" {
		return unroutePorts(ctx, cli, &Attachment{cid: cid, cname: cname, net: p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ImageRemoved(image string) error {
	return nil
}

func shouldAttachContainer(cname string, image string) bool {
	// temporarily exclude vast.ai containers and promtail
	return !strings.HasPrefix(cname, "C.") && cname != "promtail"
}
