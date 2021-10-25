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
	dhcpInterface = kingpin.Flag(
		"dhcp-interface",
		"(For bridge network) Interface where to listen for DHCPv6 PD.",
	).String()
	staticPrefix = kingpin.Flag(
		"static-prefix",
		"(For bridge network) Static IPv6 prefix for address assignment (length from /48 to /96).",
	).String()
	ipvlanInterface = kingpin.Flag(
		"ipvlan-interface",
		"(For ipvlan network) VLAN interface for the network.",
	).String()

	// testing
	test = kingpin.Flag(
		"test",
		"Perform a self-test of network attach functionality of the running daemon.",
	).Bool()
	testDhcpV4 = kingpin.Flag(
		"test-dhcpv4",
		"Perform a self-test of DHCPv4 lease/release (requires --ipvlan-interface).",
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

	var netHelper NetHelperMode = None
	if *dhcpInterface != "" || *staticPrefix != "" {
		if *dhcpInterface != "" && *staticPrefix != "" {
			log.Fatal("Please use either --dhcp-interface or --static-prefix, not both")
		}
		netHelper = Bridge
	}

	if *ipvlanInterface != "" {
		if netHelper != None {
			log.Fatal("Please use args either for bridge mode or for ipvlan, not both")
		}
		netHelper = Ipvlan

		os.MkdirAll(leaseStateDir(), 0700)
		if *testDhcpV4 {
			if err := selfTestDhcpV4(ctx, *ipvlanInterface); err != nil {
				log.Fatal(err)
			}
			return
		}

		go dhcpRenewLoopV4(ctx)
	}

	os.MkdirAll(pruneStateDir(), 0700)
	go dockerPruneLoop(ctx, cli)

	if netHelper != None {
		var netConf NetConf
		var err error

		if netHelper == Bridge {
			if *dhcpInterface != "" {
				netConf, err = bridgeNetConfFromDhcp(ctx, *dhcpInterface)
			} else {
				netConf, err = staticBridgeNetConf(*staticPrefix)
			}
		}
		if netHelper == Ipvlan {
			netConf, err = ipvlanNetConf(ctx, *ipvlanInterface)
		}
		if err != nil {
			log.Fatal(err)
		}

		dockerNet, err := selectOrCreateDockerNet(ctx, cli, &netConf)
		if err != nil {
			log.Fatal(err)
		}

		dockerEventLoop(ctx, cli, &dockerNet)

	} else {
		dockerEventLoop(ctx, cli, nil)
	}
}
