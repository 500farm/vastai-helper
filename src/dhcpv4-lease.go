package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	log "github.com/sirupsen/logrus"
)

type DhcpLeaseV4 struct {
	IfName     string
	ClientId   []byte
	HostName   string
	ClientData *nclient4.Lease
	Ttl        time.Duration
	Renewed    time.Time
}

func newLease(ifname string, clientId []byte, hostName string, lease *nclient4.Lease) DhcpLeaseV4 {
	ttl := lease.ACK.IPAddressLeaseTime(time.Hour)
	return DhcpLeaseV4{
		IfName:     ifname,
		ClientId:   clientId,
		HostName:   hostName,
		ClientData: lease,
		Ttl:        ttl,
		Renewed:    time.Now(),
	}
}

func loadLease(clientId []byte) (DhcpLeaseV4, error) {
	return loadLeaseFromFile(leaseStateFile(clientId))
}

func loadAllLeases() ([]DhcpLeaseV4, error) {
	result := []DhcpLeaseV4{}
	dir := leaseStateDir()
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return result, err
	}
	for _, file := range files {
		fileName := dir + file.Name()
		lease, err := loadLeaseFromFile(fileName)
		if err != nil {
			log.WithFields(log.Fields{"file": fileName}).Error(err)
			continue
		}
		result = append(result, lease)
	}
	return result, nil
}

func loadLeaseFromFile(file string) (DhcpLeaseV4, error) {
	j, err := ioutil.ReadFile(file)
	if err != nil {
		return DhcpLeaseV4{}, err
	}
	var r DhcpLeaseV4
	err = json.Unmarshal(j, &r)
	if err != nil {
		return DhcpLeaseV4{}, err
	}
	return r, nil
}

func (lease DhcpLeaseV4) save() error {
	j, err := json.MarshalIndent(&lease, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(leaseStateFile(lease.ClientId), j, 0600)
}

func (lease DhcpLeaseV4) Ip() net.IP {
	return lease.ClientData.ACK.YourIPAddr
}

func (lease DhcpLeaseV4) Gateway() net.IP {
	routers := lease.ClientData.ACK.Router()
	if len(routers) > 0 {
		return routers[0]
	}
	return nil
}

func (lease DhcpLeaseV4) toNetConf() NetConf {
	ack := lease.ClientData.ACK
	conf := NetConf{
		netType: None,
		v4: NetConfPrefix{
			prefix: net.IPNet{
				IP:   lease.Ip(),
				Mask: ack.SubnetMask(),
			},
			gateway:           lease.Gateway(),
			preferredLifetime: lease.Ttl / 2,
			validLifetime:     lease.Ttl,
		},
		dnsServers: ack.DNS(),
		ifname:     lease.IfName,
	}
	search := ack.DomainSearch()
	if search != nil {
		conf.dnsSearchList = search.Labels
	}
	return conf
}

func (lease DhcpLeaseV4) renew(ctx context.Context) (DhcpLeaseV4, error) {
	return dhcpLeaseV4(ctx, lease.IfName, lease.ClientId, lease.HostName, lease.Ip())
}

func (lease DhcpLeaseV4) renewIfNeeded(ctx context.Context) (DhcpLeaseV4, error) {
	if time.Now().Sub(lease.Renewed) > lease.Ttl/2 {
		return lease.renew(ctx)
	}
	return lease, nil
}

func (lease DhcpLeaseV4) release(ctx context.Context) error {
	client, err := nclient4.New(lease.IfName)
	if err != nil {
		return err
	}
	defer client.Close()

	err = client.Release(lease.ClientData, dhcpv4.WithOption(dhcpv4.OptClientIdentifier(lease.ClientId)))
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"ip":       lease.Ip(),
		"clientid": hex.EncodeToString(lease.ClientId),
	}).Info("Released DHCP lease")

	os.Remove(leaseStateFile(lease.ClientId))
	return nil
}

func (lease DhcpLeaseV4) logFields() log.Fields {
	ack := lease.ClientData.ACK
	search := ack.DomainSearch()
	searchList := []string{}
	if search != nil {
		searchList = search.Labels
	}
	return log.Fields{
		"ifname":   lease.IfName,
		"clientid": hex.EncodeToString(lease.ClientId),
		"renewed":  lease.Renewed.Format(time.RFC3339),
		"ttl":      lease.Ttl,
		"ip":       lease.Ip(),
		"gateway":  lease.Gateway(),
		"server":   ack.ServerIPAddr,
		"dns":      ack.DNS(),
		"search":   searchList,
	}
}

func leaseStateDir() string {
	return stateDir() + "lease/"
}

func leaseStateFile(clientId []byte) string {
	return stateDir() + hex.EncodeToString(clientId) + ".json"
}
