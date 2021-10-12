package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

func selfTestCmd(ctx context.Context, cli *client.Client, name string, command []string) (string, error) {
	log.WithFields(log.Fields{"cmd": strings.Join(command, " ")}).Info("Starting self-test: ", name)

	cmd := exec.Command(command[0], command[1:]...)
	stdout, err := cmd.Output()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if stderr := string(exitErr.Stderr); stderr != "" {
				return "", errors.New(stderr)
			}
			return "", fmt.Errorf("Exit code %d", exitErr.ExitCode())
		}
		return "", err
	}

	return string(stdout), nil
}

type IpAddrJson []struct {
	AddrInfo []struct {
		Local string `json:"local"`
	} `json:"addr_info"`
}

func testIpv6Presence(ctx context.Context, cli *client.Client) bool {
	out, err := selfTestCmd(ctx, cli, "IPv6 address presence", []string{"ip", "-j", "addr", "list", "scope", "global"})
	if err != nil {
		log.Error(err)
		return false
	}

	var j IpAddrJson
	err = json.Unmarshal([]byte(out), &j)
	if err != nil {
		log.Error(err)
		return false
	}

	addrs := []string{}
	success := false
	for _, t := range j {
		for _, u := range t.AddrInfo {
			addr := u.Local
			if addr != "" {
				addrs = append(addrs, addr)
				if strings.Contains(addr, ":") {
					success = true
				}
			}
		}
	}
	logger := log.WithFields(log.Fields{"addrs": strings.Join(addrs, " ")})

	if success {
		logger.Info("Test passed")
		return true
	}
	logger.Error("Test failed")
	return false
}

func testDns(ctx context.Context, cli *client.Client) bool {
	out, err := selfTestCmd(ctx, cli, "DNS", []string{"host", "ya.ru"})
	if err != nil {
		log.Error(err)
		return false
	}

	addrs := regexp.MustCompile(`[0-9a-f.:]{7,}`).FindAllString(out, -1)
	logger := log.WithFields(log.Fields{"addrs": strings.Join(addrs, " ")})

	if len(addrs) > 0 {
		logger.Info("Test passed")
		return true
	}
	logger.Error("Test failed")
	return false
}

func testIpv4Ping(ctx context.Context, cli *client.Client) bool {
	_, err := selfTestCmd(ctx, cli, "IPv4 ping", []string{"ping", "-c", "1", "-w", "1", "8.8.8.8"})
	if err != nil {
		log.Error(err)
		return false
	}
	log.Info("Test passed")
	return true
}

func testIpv6Ping(ctx context.Context, cli *client.Client) bool {
	_, err := selfTestCmd(ctx, cli, "IPv6 ping", []string{"ping", "-c", "1", "-w", "1", "2a02:6b8::2:242"})
	if err != nil {
		log.Error(err)
		return false
	}
	log.Info("Test passed")
	return true
}

func main() {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(err)
	}

	ok1 := testIpv6Presence(ctx, cli)
	ok2 := testDns(ctx, cli)
	ok3 := testIpv4Ping(ctx, cli)
	ok4 := testIpv6Ping(ctx, cli)

	if !ok1 || !ok2 || !ok3 || !ok4 {
		os.Exit(1)
	}
}
