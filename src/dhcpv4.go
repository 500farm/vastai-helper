package main

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

func dhcpLeaseV4(ctx context.Context, ifname string, clientId []byte, hostName string, prefIp net.IP) (DhcpLeaseV4, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return DhcpLeaseV4{}, err
	}
	log.WithFields(log.Fields{"ifname": iface.Name, "clientid": hex.EncodeToString(clientId)}).
		Info("DHCPv4 request")

	opts := []nclient4.ClientOpt{}
	if *debug {
		opts = append(opts, nclient4.WithDebugLogger())
	}
	client, err := nclient4.New(ifname, opts...)
	if err != nil {
		return DhcpLeaseV4{}, err
	}
	defer client.Close()

	modifiers := []dhcpv4.Modifier{
		dhcpv4.WithOption(dhcpv4.OptClientIdentifier(clientId)),
		dhcpv4.WithOption(dhcpv4.OptHostName(hostName)),
	}
	if prefIp != nil {
		modifiers = append(modifiers, dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(prefIp)))
	}
	reply, err := client.Request(ctx, modifiers...)
	if err != nil {
		return DhcpLeaseV4{}, err
	}

	lease := newLease(ifname, clientId, hostName, reply)
	log.WithFields(lease.logFields()).Info("Received DHCP lease")
	if lease.Gateway() == nil {
		return DhcpLeaseV4{}, errors.New("No gateways in DHCPv4 lease")
	}

	lease.save()
	return lease, nil
}

func dhcpReleaseV4(ctx context.Context, clientId []byte) error {
	lease, err := loadLease(clientId)
	if err != nil {
		return err
	}
	return lease.release(ctx)
}

func dhcpRenewAllV4(ctx context.Context) error {
	leases, err := loadAllLeases()
	if err != nil {
		return err
	}

	for _, lease := range leases {
		newLease, err := lease.renewIfNeeded(ctx)
		if err != nil {
			log.Error(err)
			continue
		}
		if !newLease.Ip().Equal(lease.Ip()) {
			// TODO what to do?
			log.WithFields(newLease.logFields()).
				Errorf("IP changed in DHCP lease (was %s)", lease.Ip())
		}
		if !newLease.Gateway().Equal(lease.Gateway()) {
			log.WithFields(newLease.logFields()).
				Errorf("Gateway changed in DHCP lease (was %s)", lease.Gateway())
		}
	}
	return nil
}

func dhcpRenewLoopV4(ctx context.Context) {
	for {
		time.Sleep(time.Minute)
		err := dhcpRenewAllV4(ctx)
		if err != nil {
			log.Error(err)
		}
	}
}

func makeDhcpClientId(s string) []byte {
	data, err := hex.DecodeString(s)
	if err == nil {
		return data
	}
	return []byte(s)
}
