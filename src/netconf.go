package main

import (
	"net"
	"time"

	log "github.com/sirupsen/logrus"
)

type NetConf struct {
	prefix            net.IPNet
	preferredLifetime time.Duration
	validLifetime     time.Duration
	dnsServers        []net.IP
	dnsSearchList     []string
	mode              NetHelperMode
}

type NetHelperMode int

const (
	None NetHelperMode = iota
	Bridge
	Ipvlan
)

func (conf *NetConf) logFields() log.Fields {
	return log.Fields{
		"mode":    conf.mode,
		"prefix":  conf.prefix.String(),
		"preflt":  conf.preferredLifetime,
		"validlt": conf.validLifetime,
		"dns":     conf.dnsServers,
		"search":  conf.dnsSearchList,
	}
}
