package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/insomniacslk/dhcp/iana"
)

type NetConf struct {
	prefix            net.IPNet
	preferredLifetime time.Duration
	validLifetime     time.Duration
	dnsServers        []net.IP
	dnsSearchList     []string
}

func receiveConfWithDhcp(ifname string) (NetConf, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return NetConf{}, err
	}
	log.Printf("DHCPv6 prefix delegation on interface %s (%s)", iface.Name, iface.HardwareAddr.String())

	// fix for "sendto: no route to host"
	nclient6.AllDHCPRelayAgentsAndServers.Zone = ifname

	client, err := nclient6.New(ifname, nclient6.WithDebugLogger())
	if err != nil {
		return NetConf{}, err
	}
	defer client.Close()
	ctx := context.Background()
	reply, err := client.RapidSolicit(ctx, getModifiers(iface)...)
	if err != nil {
		return NetConf{}, err
	}
	return netConfFromReply(reply)
}

func getModifiers(iface *net.Interface) []dhcpv6.Modifier {
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
	return [4]byte{0, 0, 0, 0}
}

func netConfFromReply(reply dhcpv6.DHCPv6) (NetConf, error) {
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
		conf := NetConf{
			prefix: net.IPNet{
				IP:   p.Prefix.IP,
				Mask: p.Prefix.Mask,
			},
			preferredLifetime: p.PreferredLifetime,
			validLifetime:     p.ValidLifetime,
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

func (conf *NetConf) String() string {
	t := ""
	for _, a := range conf.dnsServers {
		if t != "" {
			t += " "
		}
		t += a.String()
	}
	u := ""
	for _, a := range conf.dnsSearchList {
		if u != "" {
			u += " "
		}
		u += a
	}
	return fmt.Sprintf(
		"%s preflt=%s validlt=%s dns=[%s] search=[%s]",
		conf.prefix.String(),
		conf.preferredLifetime.String(),
		conf.validLifetime.String(),
		t,
		u,
	)
}
