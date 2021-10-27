package main

import (
	"context"
	"os"

	log "github.com/sirupsen/logrus"

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

	// auto-prune functionality
	expireTime = kingpin.Flag(
		"expire-time",
		"Expire time for stopped containers (non-VastAi), temporary images, build cache.",
	).Default("24h").Duration()
	taggedImageExpireTime = kingpin.Flag(
		"tagged-image-expire-time",
		"Prune age for tagged images.",
	).Default("168h").Duration()
	pruneInterval = kingpin.Flag(
		"prune-interval",
		"Interval between prune runs.",
	).Default("4h").Duration()

	// web server
	webServerBind = kingpin.Flag(
		"web-server-bind",
		"Web server listen address and/or port.",
	).Default(":9014").String()
)

func createDockerClient() *client.Client {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}
	return cli
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	cli := createDockerClient()
	ctx := context.Background()

	if *test {
		if err := selfTest(ctx, cli); err != nil {
			log.Fatal(err)
		}
		return
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

	// auto-prune runner
	os.MkdirAll(pruneStateDir(), 0700)
	go dockerPruneLoop(ctx, cli)

	// container info cache and web server
	err := infoCache.start(ctx, cli)
	if err != nil {
		log.Fatal(err)
	}

	if netType != None {
		// network stuff
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

		dockerNet, err := selectOrCreateDockerNet(ctx, cli, &netConf)
		if err != nil {
			log.Fatal(err)
		}

		// docker event processor
		dockerEventLoop(ctx, cli, &dockerNet)

	} else {
		// docker event processor
		dockerEventLoop(ctx, cli, nil)
	}
}
