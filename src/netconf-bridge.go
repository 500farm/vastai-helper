package main

import (
	"context"
	"errors"
	"net"
	"time"

	log "github.com/sirupsen/logrus"
)

func staticBridgeNetConf(prefix string) (NetConf, error) {
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
	log.WithFields(log.Fields{"prefix": net}).
		Info("Using static IPv6 prefix")
	return NetConf{
		mode: Bridge,
		v6: NetConfPrefix{
			prefix:  *net,
			gateway: gwAddress(*net),
		},
	}, nil
}

type DhcpKeeper struct {
	ifname  string
	netConf NetConf
	ctx     context.Context
}

func bridgeNetConfFromDhcp(ctx context.Context, ifname string) (NetConf, error) {
	keeper := DhcpKeeper{
		ifname: ifname,
		ctx:    ctx,
	}
	err := keeper.renew()
	if err != nil {
		return NetConf{}, err
	}
	go keeper.renewLoop()
	return keeper.netConf, nil
}

func (c *DhcpKeeper) renew() error {
	netConf, err := receiveConfWithDhcpV6(c.ctx, c.ifname)
	if err != nil {
		return err
	}
	log.WithFields(netConf.logFields()).Info("Received network configuration")

	len, _ := netConf.v6.prefix.Mask.Size()
	if len < 48 || len > 96 {
		return errors.New("Delegated prefix must be between /48 and /96 in length")
	}

	c.netConf = netConf
	return nil
}

func (c *DhcpKeeper) renewLoop() {
	// TODO what to do if the prefix changes or expires?
	for {
		time.Sleep(c.netConf.v6.preferredLifetime)
		for {
			err := c.renew()
			if err == nil {
				break
			}
			delay := 15 * time.Minute
			log.WithFields(log.Fields{"retry": delay}).Error(err)
			time.Sleep(delay)
		}
	}
}
