package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func selfTest(ctx context.Context, cli *client.Client) error {
	fileInfo, _ := os.Stderr.Stat()
	isTty := (fileInfo.Mode() & os.ModeCharDevice) != 0

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: "sergeycheperis/vastai-ipv6-self-test",
			Tty:   isTty,
		},
		nil, nil, nil, "self-test",
	)
	if err != nil {
		return err
	}

	defer cli.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{})

	out, err := cli.ContainerAttach(ctx, resp.ID, types.ContainerAttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return err
	}

	err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})
	if err != nil {
		return err
	}

	if isTty {
		go io.Copy(os.Stderr, out.Reader)
	} else {
		go stdcopy.StdCopy(os.Stderr, os.Stderr, out.Reader)
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case resp := <-statusCh:
		if resp.StatusCode != 0 {
			return errors.New("Self-test failed")
		}
	}

	return nil
}
