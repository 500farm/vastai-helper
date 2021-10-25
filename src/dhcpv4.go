package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

type DhcpLeaseV4 struct {
	Ifname     string
	ClientId   []byte
	ClientData *nclient4.Lease
}

func dhcpLeaseV4(ctx context.Context, ifname string, clientId string, hostName string) (NetConf, error) {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return NetConf{}, err
	}
	log.WithFields(log.Fields{"iface": iface.Name, "clientid": clientId}).
		Info("DHCPv4 IP request")

	client, err := nclient4.New(ifname, nclient4.WithDebugLogger())
	if err != nil {
		return NetConf{}, err
	}
	defer client.Close()
	reply, err := client.Request(
		ctx,
		dhcpv4.WithOption(dhcpv4.OptClientIdentifier(makeDhcpClientId(clientId))),
		dhcpv4.WithOption(dhcpv4.OptHostName(hostName)),
	)
	if err != nil {
		return NetConf{}, err
	}
	saveLease(ifname, clientId, reply)
	return netConfFromAckV4(reply.ACK, ifname)
}

func dhcpReleaseV4(ctx context.Context, clientId string) error {
	lease, err := loadLease(clientId)
	if err != nil {
		return err
	}
	if lease == nil {
		return nil
	}

	client, err := nclient4.New(lease.Ifname, nclient4.WithDebugLogger())
	if err != nil {
		return err
	}
	defer client.Close()

	err = client.Release(lease.ClientData, dhcpv4.WithOption(dhcpv4.OptClientIdentifier(lease.ClientId)))
	if err != nil {
		return err
	}
	deleteLease(clientId)
	return nil
}

func selfTestDhcpV4(ctx context.Context, ifname string) error {
	clientId := "test-client-id"

	conf, err := dhcpLeaseV4(ctx, ifname, clientId, "test-host-name")
	if err != nil {
		return err
	}
	log.WithFields(conf.logFields()).Info("Received DHCP lease")

	j, err := ioutil.ReadFile(leaseStateFile(clientId))
	if err != nil {
		return err
	}
	log.Info("State file contents: " + string(j))

	log.Info("Waiting 5 seconds before release")
	time.Sleep(5 * time.Second)

	err = dhcpReleaseV4(ctx, clientId)
	if err != nil {
		return err
	}
	return nil
}

func makeDhcpClientId(s string) []byte {
	data, err := hex.DecodeString(s)
	if err == nil {
		return data
	}
	return []byte(s)
}

func netConfFromAckV4(ack *dhcpv4.DHCPv4, ifname string) (NetConf, error) {
	ttl := ack.IPAddressLeaseTime(time.Hour)
	routers := ack.Router()
	if len(routers) == 0 {
		return NetConf{}, errors.New("No routers in DHCPv4 lease")
	}
	conf := NetConf{
		mode: None,
		v4: NetConfPrefix{
			prefix: net.IPNet{
				IP:   ack.YourIPAddr,
				Mask: ack.SubnetMask(),
			},
			gateway:           routers[0],
			preferredLifetime: ttl / 2,
			validLifetime:     ttl,
		},
		dnsServers: ack.DNS(),
		ifname:     ifname,
	}
	search := ack.DomainSearch()
	if search != nil {
		conf.dnsSearchList = search.Labels
	}
	return conf, nil
}

func saveLease(ifname string, clientId string, lease *nclient4.Lease) error {
	r := DhcpLeaseV4{
		Ifname:     ifname,
		ClientId:   makeDhcpClientId(clientId),
		ClientData: lease,
	}
	j, err := json.MarshalIndent(r, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(leaseStateFile(clientId), j, 0600)
}

func loadLease(clientId string) (*DhcpLeaseV4, error) {
	j, err := ioutil.ReadFile(leaseStateFile(clientId))
	if err != nil {
		return nil, err
	}
	var r DhcpLeaseV4
	err = json.Unmarshal(j, &r)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func deleteLease(clientId string) {
	os.Remove(leaseStateFile(clientId))
}

func leaseStateDir() string {
	return stateDir() + "lease/"
}

func leaseStateFile(clientId string) string {
	return stateDir() + clientId + ".json"
}
