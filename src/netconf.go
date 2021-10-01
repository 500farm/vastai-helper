package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

type NetConf struct {
	prefix            net.IPNet
	preferredLifetime time.Duration
	validLifetime     time.Duration
	dnsServers        []net.IP
	dnsSearchList     []string
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

func staticNetConf(prefix string) (NetConf, error) {
	_, net, err := net.ParseCIDR(prefix)
	if err != nil {
		return NetConf{}, err
	}
	len, total := net.Mask.Size()
	if total != 128 {
		return NetConf{}, errors.New("Please specify an IPv6 prefix")
	}
	if len < 48 || len > 96 {
		return NetConf{}, errors.New("Please specify an IPv6 prefix between /48 and /96 in length")
	}
	log.Printf("Using static IPv6 prefix: %s", net.String())
	return NetConf{prefix: *net}, nil
}
