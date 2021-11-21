package netattach

import (
	"context"
	"os"
	"strings"

	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
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
	ctx      context.Context
	cli      *client.Client
	enabled  bool
	net      DockerNet
	stateDir string
}

func NewPlugin(ctx context.Context, cli *client.Client, stateDir string) *NetAttachPlugin {
	return &NetAttachPlugin{
		ctx:      ctx,
		cli:      cli,
		stateDir: stateDir + "lease/",
	}
}

func (p *NetAttachPlugin) ContainerDiscovered(cid string, cname string, image string) error {
	return nil
}

func (p *NetAttachPlugin) Start() error {
	if *test {
		// self-test mode
		if err := selfTest(p.ctx, p.cli); err != nil {
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

		leaseStateDir = p.stateDir
		os.MkdirAll(p.stateDir, 0700)

		dhcpDebug = *debug

		if netType == Bridge {
			if *ipv6Prefix != "" {
				netConfV6, err = staticNetConfV6(*ipv6Prefix, "")
			} else {
				netConfV6, err = dhcpNetConfV6(p.ctx, *netInterface, false)
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
			netConfV4, err = dhcpNetConfV4(p.ctx, *netInterface)
			if err != nil {
				log.Fatal(err)
			}
			go dhcpRenewLoopV4(p.ctx)
		}

		netConf := mergeNetConf(netConfV4, netConfV6, netType)
		log.WithFields(netConf.logFields()).Info("Detected network configuration")

		p.net, err = selectOrCreateDockerNet(p.ctx, p.cli, &netConf)
		if err != nil {
			log.Fatal(err)
		}

		p.enabled = true
	}

	return nil
}

func (p *NetAttachPlugin) ContainerCreated(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) {
		return attachContainerToNet(p.ctx, p.cli, &Attachment{cid: cid, cname: cname, net: &p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ContainerDestroyed(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) {
		return detachContainerFromNet(p.ctx, p.cli, &Attachment{cid: cid, cname: cname, net: &p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ContainerStarted(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) &&
		p.net.driver == "bridge" {
		return routePorts(p.ctx, p.cli, &Attachment{cid: cid, cname: cname, net: &p.net})
	}
	return nil
}

func (p *NetAttachPlugin) ContainerStopped(cid string, cname string, image string) error {
	if p.enabled &&
		shouldAttachContainer(cname, image) &&
		p.net.driver == "bridge" {
		return unroutePorts(p.ctx, p.cli, &Attachment{cid: cid, cname: cname, net: &p.net})
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
