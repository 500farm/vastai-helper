package main

import (
	"context"
	"errors"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func selfTest(ctx context.Context, cli *client.Client) error {
	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: "sergeycheperis/vastai-ipv6-self-test",
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

	go stdcopy.StdCopy(os.Stdout, os.Stderr, out.Reader)

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
