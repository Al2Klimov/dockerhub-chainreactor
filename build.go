package main

import (
	"context"
	"github.com/docker/docker/api/types"
	docker "github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
	"io"
	"strings"
	"sync"
)

func build(hub []hubConfig) {
	images := map[string]struct{}{}

	for _, h := range hub {
		for _, base := range h.Base {
			images[base] = struct{}{}
		}
	}

	if len(images) < 1 {
		log.Warn("No base images to pull")
		return
	}

	log.Trace("Setting up Docker client")

	client, errNC := docker.NewEnvClient()
	if errNC != nil {
		log.WithFields(log.Fields{"error": jsonableError{errNC}}).Error("Couldn't set up Docker client")
		return
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(background)

	wg.Add(len(images))

	for image := range images {
		go pull(client, image, ctx, cancel, &wg)
	}

	wg.Wait()
}

func pull(client *docker.Client, image string, ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()

	switch strings.Count(image, "/") {
	case 0:
		image = "library/" + image
		fallthrough
	case 1:
		image = "docker.io/" + image
	}

	log.WithFields(log.Fields{"image": image}).Debug("Pulling image")

	resp, errIP := client.ImagePull(ctx, image, types.ImagePullOptions{})
	if errIP != nil {
		if errIP != context.Canceled {
			log.WithFields(log.Fields{"image": image, "error": jsonableError{errIP}}).Error("Couldn't pull image")
			cancel()
		}

		return
	}

	defer resp.Close()

	io.Copy(nullWriter{}, resp)
}
