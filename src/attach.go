package main

import (
	"context"
	"crypto/rand"
	"log"
	"net"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func attachContainerToNet(ctx context.Context, cli *client.Client, cid string, cname string, net DockerNet) error {
	ip := randomIp(net.prefix).String()
	log.Printf("%s: attaching to network %s with IP %s", cname, net.name, ip)
	return cli.NetworkConnect(ctx, net.id, cid, &network.EndpointSettings{
		IPAMConfig: &network.EndpointIPAMConfig{
			IPv6Address: ip,
		},
	})
}

func cleanupContainer(ctx context.Context, cli *client.Client, cid string) error {
	return nil
}

func randomIp(prefix net.IPNet) net.IP {
	result := make([]byte, 16)
	rand.Read(result)
	for i := 0; i < 16; i++ {
		result[i] = (prefix.IP[i] & prefix.Mask[i]) | (result[i] &^ prefix.Mask[i])
	}
	return result
}
