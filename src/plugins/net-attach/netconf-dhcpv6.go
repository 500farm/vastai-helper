package plugin

import (
	"context"
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
)

type DhcpKeeper struct {
	ifname       string
	netConf      NetConf
	sharedPrefix bool
	ctx          context.Context
}

func dhcpNetConfV6(ctx context.Context, ifname string, sharedPrefix bool) (NetConf, error) {
	keeper := DhcpKeeper{
		ifname:       ifname,
		ctx:          ctx,
		sharedPrefix: sharedPrefix,
	}
	err := keeper.renew()
	if err != nil {
		return NetConf{}, err
	}
	go keeper.renewLoop()
	return keeper.netConf, nil
}

func (c *DhcpKeeper) renew() error {
	netConf, err := receiveConfWithDhcpV6(c.ctx, c.ifname, c.sharedPrefix)
	if err != nil {
		return err
	}

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
