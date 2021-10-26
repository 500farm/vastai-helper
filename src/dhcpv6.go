package main

import (
	"context"
	"errors"
	"net"

	log "github.com/sirupsen/logrus"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/insomniacslk/dhcp/iana"
)

func receiveConfWithDhcpV6(ctx context.Context, ifname string) (NetConf, error) {
	// TODO does non-rapid solicit work in insomniacslk/dhcp/dhcpv6 library?

	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return NetConf{}, err
	}
	log.WithFields(log.Fields{"ifname": iface.Name, "addr": iface.HardwareAddr.String()}).
		Info("DHCPv6 prefix delegation")

	// fix for "sendto: no route to host"
	nclient6.AllDHCPRelayAgentsAndServers.Zone = ifname

	client, err := nclient6.New(ifname, nclient6.WithDebugLogger())
	if err != nil {
		return NetConf{}, err
	}
	defer client.Close()
	reply, err := client.RapidSolicit(ctx, getModifiersV6(iface)...)
	if err != nil {
		return NetConf{}, err
	}
	return netConfFromReplyV6(reply, ifname)
}

func getModifiersV6(iface *net.Interface) []dhcpv6.Modifier {
	return []dhcpv6.Modifier{
		dhcpv6.WithClientID(generateDuid(iface)),
		dhcpv6.WithIAPD(generateIaid()),
	}
}

func generateDuid(iface *net.Interface) dhcpv6.Duid {
	return dhcpv6.Duid{
		Type:          dhcpv6.DUID_LL,
		HwType:        iana.HWTypeEthernet,
		LinkLayerAddr: iface.HardwareAddr,
	}
}

func generateIaid() [4]byte {
	return [4]byte{76, 61, 73, 74}
}

func netConfFromReplyV6(reply dhcpv6.DHCPv6, ifname string) (NetConf, error) {
	d, err := reply.GetInnerMessage()
	if err != nil {
		return NetConf{}, err
	}
	iapd := d.Options.OneIAPD()
	if iapd == nil {
		return NetConf{}, errors.New("No option IA PD found")
	}
	if st := iapd.Options.Status(); st != nil {
		if st.StatusCode == 6 {
			return NetConf{}, errors.New("No prefix available for delegation")
		}
		return NetConf{}, errors.New(st.String())
	}
	for _, p := range iapd.Options.Prefixes() {
		if p.Prefix == nil {
			continue
		}
		prefix := net.IPNet{
			IP:   p.Prefix.IP,
			Mask: p.Prefix.Mask,
		}
		conf := NetConf{
			netType: Bridge,
			v6: NetConfPrefix{
				prefix:            prefix,
				gateway:           gwAddress(prefix),
				preferredLifetime: p.PreferredLifetime,
				validLifetime:     p.ValidLifetime,
			},
			ifname: ifname,
		}
		// add DNS configuration
		dns := d.Options.DNS()
		if len(dns) != 0 {
			conf.dnsServers = dns
		}
		domains := d.Options.DomainSearchList()
		if domains != nil {
			conf.dnsSearchList = domains.Labels
		}
		return conf, nil
	}
	return NetConf{}, errors.New("No prefixes returned in IA PD")
}
