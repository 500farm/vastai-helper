package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

func ipvlanNetConf(ctx context.Context, ifname string) (NetConf, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return NetConf{}, err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return NetConf{}, err
	}
	log.WithFields(log.Fields{"iface": iface.Name, "addr": iface.HardwareAddr.String(), "ips": addrs}).
		Info("Using interface")

	netConf := NetConf{
		mode:   Ipvlan,
		ifname: ifname,
	}
	for _, addr := range addrs {
		ip, net, _ := net.ParseCIDR(addr.String())
		if ip.IsGlobalUnicast() {
			ipv6 := ip.To4() == nil
			gw, err := findGateway(ifname, ipv6)
			if err != nil {
				return NetConf{}, err
			}
			if ipv6 {
				netConf.v6.prefix = *net
				netConf.v6.gateway = gw
			} else {
				netConf.v4.prefix = *net
				netConf.v4.gateway = gw
			}
		}
	}
	log.WithFields(netConf.logFields()).Info("Using network configuration")

	if !netConf.hasV4() || !netConf.hasV6() {
		return NetConf{}, errors.New("Ipvlan interface must be configured for both IPv4 and IPv6")
	}
	return netConf, nil
}

type IpRoute struct {
	Gateway string `json:"gateway"`
}

func findGateway(ifname string, ipv6 bool) (net.IP, error) {
	var mode string
	if ipv6 {
		mode = "-6"
	} else {
		mode = "-4"
	}
	cmd := exec.Command("/usr/sbin/ip", "-j", mode, "route", "show", "default", "dev", ifname)
	j, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var routes []IpRoute
	err = json.Unmarshal(j, &routes)
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, errors.New("No default gateways found for " + ifname)
	}
	if len(routes) > 1 {
		return nil, errors.New("Multiple default gateways found for " + ifname)
	}
	return net.ParseIP(routes[0].Gateway), nil
}
