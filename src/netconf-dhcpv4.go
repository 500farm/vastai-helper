package main

import (
	"context"
)

func dhcpNetConfV4(ctx context.Context, ifname string) (NetConf, error) {
	lease, err := dhcpLeaseV4(ctx, ifname, []byte("vastai-helper"), "vastai-helper", nil)
	if err != nil {
		return NetConf{}, err
	}
	lease.release(ctx)
	return lease.toNetConf(), nil
}
