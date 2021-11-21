package plugin

import (
	"errors"
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
	netType       NetType
	ifname        string
}

type NetType int

const (
	None NetType = iota
	Bridge
	Ipvlan
)

func (conf *NetConf) hasV4() bool {
	return conf.v4.prefix.IP != nil
}

func (conf *NetConf) hasV6() bool {
	return conf.v6.prefix.IP != nil
}

func autoGwAddress(prefix net.IPNet) net.IP {
	result, _ := cidr.Host(&prefix, 1)
	return result
}

func (p NetConfPrefix) logFields() log.Fields {
	return log.Fields{
		"prefix":  p.prefix.String(),
		"gw":      p.gateway.String(),
		"preflt":  p.preferredLifetime,
		"validlt": p.validLifetime,
	}
}

func (conf NetConf) logFields() log.Fields {
	return log.Fields{
		"type":   conf.netType,
		"ifname": conf.ifname,

		"v6.prefix":  conf.v6.prefix.String(),
		"v6.gw":      conf.v6.gateway.String(),
		"v6.preflt":  conf.v6.preferredLifetime,
		"v6.validlt": conf.v6.validLifetime,

		"v4.prefix":  conf.v4.prefix.String(),
		"v4.gw":      conf.v4.gateway.String(),
		"v4.preflt":  conf.v4.preferredLifetime,
		"v4.validlt": conf.v4.validLifetime,

		"dns":    conf.dnsServers,
		"search": conf.dnsSearchList,
	}
}

func mergeNetConf(conf4 NetConf, conf6 NetConf, netType NetType) NetConf {
	result := conf4
	result.netType = netType
	result.v6 = conf6.v6
	if result.ifname == "" {
		result.ifname = conf6.ifname
	}
	result.dnsServers = append(append([]net.IP{}, conf6.dnsServers...), conf4.dnsServers...)
	if len(result.dnsSearchList) == 0 {
		result.dnsSearchList = conf6.dnsSearchList
	}
	return result
}

func staticNetConfV6(prefix string, gw string) (NetConf, error) {
	_, n, err := net.ParseCIDR(prefix)
	if err != nil {
		return NetConf{}, err
	}
	len, total := n.Mask.Size()
	if total != 128 {
		return NetConf{}, errors.New("Please specify an IPv6 prefix")
	}
	if len < 48 || len > 96 {
		return NetConf{}, errors.New("Please specify an IPv6 prefix between /48 and /96 in length")
	}

	var gwIp net.IP
	if gw != "" {
		gwIp = net.ParseIP(gw)
	} else {
		gwIp = autoGwAddress(*n)
	}

	log.WithFields(log.Fields{"prefix": n, "gw": gwIp}).
		Info("Using static IPv6 prefix")
	return NetConf{
		v6: NetConfPrefix{
			prefix:  *n,
			gateway: gwIp,
		},
	}, nil
}
