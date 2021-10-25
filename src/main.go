package main

import (
	"context"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/docker/docker/client"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	dhcpInterface = kingpin.Flag(
		"dhcp-interface",
		"Interface where to listen for DHCPv6 PD.",
	).String()
	staticPrefix = kingpin.Flag(
		"static-prefix",
		"Static IPv6 prefix for address assignment (length from /48 to /96).",
	).String()
	ipvlan = kingpin.Flag(
		"ipvlan",
		"Use ipvlan network instead of bridge",
	).Bool()
	test = kingpin.Flag(
		"test",
		"Perform a self-test of network attach functionality of the running daemon.",
	).Bool()
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

	if *dhcpInterface != "" && *staticPrefix != "" {
		log.Fatal("Please specify either --dhcp-interface or --static-prefix, not both")
	}

	os.MkdirAll(pruneStateDir(), 0700)
	go dockerPruneLoop(ctx, cli)

	useNetHelper := *dhcpInterface != "" || *staticPrefix != ""

	if useNetHelper {
		var netConf NetConf
		var err error
		if *dhcpInterface != "" {
			netConf, err = startDhcp(ctx, *dhcpInterface)
		} else {
			netConf, err = staticNetConf(*staticPrefix)
		}
		if err != nil {
			log.Fatal(err)
		}

		driver := "bridge"
		if *ipvlan {
			driver = "ipvlan"
		}
		dockerNet, err := selectOrCreateDockerNet(ctx, cli, driver, &netConf)
		if err != nil {
			log.Fatal(err)
		}

		dockerEventLoop(ctx, cli, &dockerNet)

	} else {
		dockerEventLoop(ctx, cli, nil)
	}
}
