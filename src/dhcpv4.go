package main

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

func dhcpLeaseV4(ctx context.Context, ifname string, clientId string, hostName string) (NetConf, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return NetConf{}, err
	}
	log.WithFields(log.Fields{"iface": iface.Name, "clientid": clientId}).
		Info("DHCPv4 IP request")

	client, err := nclient4.New(ifname, nclient4.WithDebugLogger())
	if err != nil {
		return NetConf{}, err
	}
	defer client.Close()
	reply, err := client.Request(
		ctx,
		dhcpv4.WithOption(dhcpv4.OptClientIdentifier(makeDhcpClientId(clientId))),
		dhcpv4.WithOption(dhcpv4.OptHostName(hostName)),
	)
	if err != nil {
		return NetConf{}, err
	}
	return netConfFromReplyV4(reply.ACK, ifname)

	// TODO renew and release
}

func makeDhcpClientId(s string) []byte {
	data, err := hex.DecodeString(s)
	if err == nil {
		return data
	}
	return []byte(s)
}

func netConfFromReplyV4(reply *dhcpv4.DHCPv4, ifname string) (NetConf, error) {
	ttl := reply.IPAddressLeaseTime(time.Hour)
	routers := reply.Router()
	if len(routers) == 0 {
		return NetConf{}, errors.New("No routers in DHCPv4 lease")
	}
	conf := NetConf{
		mode: None,
		v4: NetConfPrefix{
			prefix: net.IPNet{
				IP:   reply.YourIPAddr,
				Mask: reply.SubnetMask(),
			},
			gateway:           routers[0],
			preferredLifetime: ttl / 2,
			validLifetime:     ttl,
		},
		dnsServers: reply.DNS(),
		ifname:     ifname,
	}
	search := reply.DomainSearch()
	if search != nil {
		conf.dnsSearchList = search.Labels
	}
	return conf, nil
}
