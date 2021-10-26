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
	staticIpv6Prefix = kingpin.Flag(
		"static-ipv6-prefix",
		"Static IPv6 prefix for address assignment (length from /48 to /96).",
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
		if err := selfTest(ctx, createDockerClient()); err != nil {
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
		log.Fatal("For network functionality --net-interface is required.")
	}

	if netType == Ipvlan {
		os.MkdirAll(leaseStateDir(), 0700)
		go dhcpRenewLoopV4(ctx)
	}

	os.MkdirAll(pruneStateDir(), 0700)
	go dockerPruneLoop(ctx, cli)

	if netType != None {
		var netConfV6, netConfV4 NetConf
		var err error

		if *staticIpv6Prefix != "" {
			netConfV6, err = staticNetConfV6(*staticIpv6Prefix)
		} else {
			netConfV6, err = dhcpNetConfV6(ctx, *netInterface, netType == Ipvlan)
		}
		if err != nil {
			log.Fatal(err)
		}

		if netType == Ipvlan {
			netConfV4, err = dhcpNetConfV4(ctx, *netInterface)
			if err != nil {
				log.Fatal(err)
			}
		}

		netConf := mergeNetConf(netConfV4, netConfV6, netType)
		log.WithFields(netConf.logFields()).Info("Detected network configuration")

		dockerNet, err := selectOrCreateDockerNet(ctx, cli, &netConf)
		if err != nil {
			log.Fatal(err)
		}

		dockerEventLoop(ctx, cli, &dockerNet)

	} else {
		dockerEventLoop(ctx, cli, nil)
	}
}
