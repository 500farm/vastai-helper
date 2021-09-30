package main

import (
	"log"
	"os"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	dhcpInterface = kingpin.Flag(
		"dhcp-interface",
		"Interface where to listen for DHCP-PD.",
	).Required().String()

	netConf NetConf
)

func dhcpRenew() error {
	conf, err := receiveConfWithDhcp(*dhcpInterface)
	if err != nil {
		log.Printf("DHCPv6 error: %v", err)
		return err
	}
	log.Printf("Received network configuration:\n%s", conf.String())
	netConf = conf
	return nil
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	err := dhcpRenew()
	if err != nil {
		os.Exit(1)
	}

	go func() {
		for {
			time.Sleep(netConf.preferredLifetime)
			for {
				if err := dhcpRenew(); err == nil {
					break
				}
				delay := 15 * time.Minute
				log.Printf("Will retry in %s", delay.String())
				time.Sleep(delay)
			}
		}
	}()
}
