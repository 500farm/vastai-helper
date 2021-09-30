package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/client6"
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
	attempts := 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		log.Printf("Attempt %d of %d", attempt+1, attempts)
		conf, err := tryReceiveConfWithDhcp(iface)
		if err == nil {
			return conf, nil
		} else {
			log.Printf("Error: %v", err)
			lastErr = err
		}
	}
	return NetConf{}, lastErr
}

func tryReceiveConfWithDhcp(iface *net.Interface) (NetConf, error) {
	c, err := createClient(iface)
	advertise, err := doSolicit(c, iface)
	if err != nil {
		return NetConf{}, err
	}
	reply, err := doRequest(c, iface, advertise)
	if err != nil {
		return NetConf{}, err
	}
	return netConfFromReply(reply)
}

func createClient(iface *net.Interface) (*client6.Client, error) {
	llAddr, err := dhcpv6.GetLinkLocalAddr(iface.Name)
	if err != nil {
		return nil, err
	}
	laddr := net.UDPAddr{
		IP:   llAddr,
		Port: dhcpv6.DefaultClientPort,
		Zone: iface.Name,
	}
	raddr := net.UDPAddr{
		IP:   dhcpv6.AllDHCPRelayAgentsAndServers,
		Port: dhcpv6.DefaultServerPort,
		Zone: iface.Name,
	}
	c := client6.NewClient()
	c.LocalAddr = &laddr
	c.RemoteAddr = &raddr
	return c, nil
}

func doSolicit(c *client6.Client, iface *net.Interface) (dhcpv6.DHCPv6, error) {
	solicit, advertise, err := c.Solicit(iface.Name, getModifiers(iface)...)
	if solicit != nil {
		printSent(solicit)
	}
	if advertise != nil {
		printReceived(advertise)
	}
	if err != nil {
		return nil, err
	}

	// Decapsulate advertise if it's relayed before passing it to Request
	if advertise.IsRelay() {
		advertiseRelay := advertise.(*dhcpv6.RelayMessage)
		advertise, err = advertiseRelay.GetInnerMessage()
		if err != nil {
			return nil, err
		}
		printReceived(advertise)
	}

	return advertise, nil
}

func doRequest(c *client6.Client, iface *net.Interface, advertise dhcpv6.DHCPv6) (dhcpv6.DHCPv6, error) {
	request, reply, err := c.Request(
		iface.Name,
		advertise.(*dhcpv6.Message),
		getModifiers(iface)...,
	)
	if request != nil {
		printSent(request)
	}
	if reply != nil {
		printReceived(reply)
	}
	if err != nil {
		return nil, err
	}
	return reply, nil
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

func printSent(m dhcpv6.DHCPv6) {
	log.Print("-> ", m.Summary())
}

func printReceived(m dhcpv6.DHCPv6) {
	log.Print("<- ", m.Summary())
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
