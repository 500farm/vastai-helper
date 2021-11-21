package netattach

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	log "github.com/sirupsen/logrus"
)

func dhcpNetConfV4(ctx context.Context, ifname string) (NetConf, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return NetConf{}, err
	}
	log.WithFields(log.Fields{"ifname": iface.Name}).Info("DHCPv4 discover")

	opts := []nclient4.ClientOpt{}
	if dhcpDebug {
		opts = append(opts, nclient4.WithDebugLogger())
	}
	client, err := nclient4.New(ifname, opts...)
	if err != nil {
		return NetConf{}, err
	}
	defer client.Close()

	modifiers := []dhcpv4.Modifier{
		dhcpv4.WithOption(dhcpv4.OptClientIdentifier([]byte("vastai-helper"))),
	}
	offer, err := client.DiscoverOffer(ctx, modifiers...)
	if err != nil {
		return NetConf{}, err
	}

	return netConfFromOffer(offer, ifname)
}

func netConfFromOffer(offer *dhcpv4.DHCPv4, ifname string) (NetConf, error) {
	mask := offer.SubnetMask()
	routers := offer.Router()
	ttl := offer.IPAddressLeaseTime(time.Hour)
	if len(routers) == 0 {
		return NetConf{}, errors.New("DHCPv4 offer does not contain a router")
	}
	conf := NetConf{
		v4: NetConfPrefix{
			prefix: net.IPNet{
				IP:   offer.YourIPAddr.Mask(mask),
				Mask: mask,
			},
			gateway:           routers[0],
			preferredLifetime: ttl / 2,
			validLifetime:     ttl,
		},
		dnsServers: offer.DNS(),
		ifname:     ifname,
	}
	search := offer.DomainSearch()
	if search != nil {
		conf.dnsSearchList = search.Labels
	}
	return conf, nil

}
