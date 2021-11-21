package main

type Plugin interface {
	Start() error
	ContainerCreated(cid string, cname string, image string) error
	ContainerDestroyed(cid string, cname string, image string) error
	ContainerStarted(cid string, cname string, image string) error
	ContainerStopped(cid string, cname string, image string) error
	ImageRemoved(image string) error
}
