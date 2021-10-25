package main

import (
	"net"
	"time"

	"github.com/apparentlymart/go-cidr/cidr"
	log "github.com/sirupsen/logrus"
)

type NetConfPrefix struct {
	prefix            net.IPNet
	preferredLifetime time.Duration
	validLifetime     time.Duration
	gateway           net.IP
}
type NetConf struct {
	v4, v6        NetConfPrefix
	dnsServers    []net.IP
	dnsSearchList []string
	mode          NetHelperMode
}

type NetHelperMode int

const (
	None NetHelperMode = iota
	Bridge
	Ipvlan
)

func (conf *NetConf) hasV4() bool {
	return conf.v4.prefix.IP != nil
}

func (conf *NetConf) hasV6() bool {
	return conf.v6.prefix.IP != nil
}

func gwAddress(prefix net.IPNet) net.IP {
	result, _ := cidr.Host(&prefix, 1)
	return result
}

func (conf *NetConf) logFields() log.Fields {
	return log.Fields{
		"mode":       conf.mode,
		"v6.prefix":  conf.v6.prefix.String(),
		"v6.preflt":  conf.v6.preferredLifetime,
		"v6.validlt": conf.v6.validLifetime,
		"v6.gw":      conf.v6.gateway.String(),
		"v4.prefix":  conf.v4.prefix.String(),
		"v4.gw":      conf.v4.gateway.String(),
		"dns":        conf.dnsServers,
		"search":     conf.dnsSearchList,
	}
}
