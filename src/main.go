package main

import (
	"log"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/client6"
	"github.com/insomniacslk/dhcp/iana"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	dhcpInterface = kingpin.Flag(
		"dhcp-interface",
		"Interface where to listen for DHCP-PD.",
	).Required().String()
	prefixLength = kingpin.Flag(
		"prefix-length",
		"Length of the prefix queried from the DHCP-PD server.",
	).Default("64").Int()
)

// test duid
func generateDuid() dhcpv6.Duid {
	return dhcpv6.Duid{
		Type:          dhcpv6.DUID_LL,
		HwType:        iana.HWTypeEthernet,
		LinkLayerAddr: net.HardwareAddr([]byte{0xfa, 0xce, 0xb0, 0x00, 0x00, 0x0c}),
	}
}

// based on https://github.com/insomniacslk/exdhcp/blob/master/dhclient/main.go
func dhclient6(ifname string, attempts int, verbose bool) (error, error) {
	if attempts < 1 {
		attempts = 1
	}
	llAddr, err := dhcpv6.GetLinkLocalAddr(ifname)
	if err != nil {
		return nil, err
	}
	laddr := net.UDPAddr{
		IP:   llAddr,
		Port: dhcpv6.DefaultClientPort,
		Zone: ifname,
	}
	raddr := net.UDPAddr{
		IP:   dhcpv6.AllDHCPRelayAgentsAndServers,
		Port: dhcpv6.DefaultServerPort,
		Zone: ifname,
	}
	c := client6.NewClient()
	c.LocalAddr = &laddr
	c.RemoteAddr = &raddr
	var conv []dhcpv6.DHCPv6
	for attempt := 0; attempt < attempts; attempt++ {
		log.Printf("Attempt %d of %d", attempt+1, attempts)
		conv, err = c.Exchange(ifname, dhcpv6.WithClientID(generateDuid()), dhcpv6.WithIAPD([4]byte{0, 0, 0, 0}))
		if err != nil && attempt < attempts {
			log.Printf("Error: %v", err)
			continue
		}
		break
	}
	if verbose {
		for _, m := range conv {
			log.Print(m.Summary())
		}
	}
	if err != nil {
		return nil, err
	}
	// extract the network configuration
	//netconf, err := netboot.ConversationToNetconf(conv)
	return nil, err
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	log.Printf("Starting DHCPv6 client on interface %s", *dhcpInterface)

	netconf, err := dhclient6(*dhcpInterface, 3, true)
	if err != nil {
		log.Printf("%+v", err)
	} else {
		// configure the interface
		log.Printf("Received network configuration:")
		log.Printf("%+v", netconf)
	}
}
