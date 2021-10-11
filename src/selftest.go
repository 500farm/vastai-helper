package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	log "github.com/sirupsen/logrus"
)

func selfTestCmd(ctx context.Context, cli *client.Client, name string, cmd []string) (string, error) {
	log.WithFields(log.Fields{"cmd": strings.Join(cmd, " ")}).Info("Starting self-test: ", name)
	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Cmd:   cmd,
			Image: "jonlabelle/network-tools",
		},
		nil, nil, nil, "",
	)
	if err != nil {
		return "", err
	}

	defer cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{})

	err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})
	if err != nil {
		return "", err
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	exitCode := 0
	select {
	case err := <-errCh:
		if err != nil {
			return "", err
		}
	case resp := <-statusCh:
		if resp.StatusCode != 0 {
			exitCode = int(resp.StatusCode)
		}
	}

	reader, err := cli.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", err
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	_, err = stdcopy.StdCopy(stdout, stderr, reader)
	if err != nil {
		return "", err
	}

	if exitCode != 0 {
		if stderrStr := stderr.String(); stderrStr != "" {
			return "", errors.New(stderrStr)
		} else {
			return "", fmt.Errorf("Exit code %d", exitCode)
		}
	}

	return stdout.String(), nil
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

func selfTest(ctx context.Context, cli *client.Client) bool {
	ok1 := testIpv6Presence(ctx, cli)
	ok2 := testDns(ctx, cli)
	ok3 := testIpv4Ping(ctx, cli)
	ok4 := testIpv6Ping(ctx, cli)
	return ok1 && ok2 && ok3 && ok4
}
