package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/gob"
	fatomic "github.com/Al2Klimov/atomic"
	"github.com/docker/docker/api/types"
	hashstruct "github.com/mitchellh/hashstructure"
	docker "github.com/moby/moby/client"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

func build(hub []hubConfig) {
	images := map[string]struct{}{}

	for _, h := range hub {
		for _, base := range h.Base {
			images[normalize(base)] = struct{}{}
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

	{
		var wg sync.WaitGroup
		ctx, cancel := context.WithCancel(background)

		wg.Add(len(images))

		for image := range images {
			go pull(client, image, ctx, cancel, &wg)
		}

		wg.Wait()
	}

	log.Debug("Listing images")

	imgInfo, errIL := client.ImageList(background, types.ImageListOptions{})
	if errIL != nil {
		log.WithFields(log.Fields{"error": jsonableError{errIL}}).Error("Couldn't list images")
		return
	}

	ids := map[string]string{}
	for _, img := range imgInfo {
		if len(img.RepoTags) > 0 {
			ids[normalize(img.RepoTags[0])] = img.ID
		}
	}

	urls := map[string]map[string]string{}
	for _, h := range hub {
		url, ok := urls[h.Post]
		if !ok {
			url = map[string]string{}
			urls[h.Post] = url
		}

		for _, base := range h.Base {
			base = normalize(base)
			url[base] = ids[base]
		}
	}

	next := make(state, len(urls))
	for url, imgs := range urls {
		hash := sha1.New()
		if _, errHs := hashstruct.Hash(imgs, &hashstruct.HashOptions{Hasher: hashWrapper64{hash}}); errHs != nil {
			log.WithFields(log.Fields{"value": imgs, "error": jsonableError{errHs}}).Error("Couldn't hash value")
			return
		}

		var sum [sha1.Size]byte
		copy(sum[:], hash.Sum(nil))
		next[url] = sum
	}

	var current state
	log.WithFields(log.Fields{"file": statePath}).Debug("Reading state")

	if stateFile, errOp := os.Open(statePath); errOp == nil {
		if errDc := gob.NewDecoder(bufio.NewReader(stateFile)).Decode(&current); errDc != nil {
			log.WithFields(log.Fields{"file": statePath, "error": jsonableError{errDc}}).Warn("Couldn't read state")
			current = state{}
		}

		stateFile.Close()
	} else if os.IsNotExist(errOp) {
		current = state{}
	} else {
		log.WithFields(log.Fields{"file": statePath, "error": jsonableError{errOp}}).Error("Couldn't read state")
		return
	}

	{
		var wg sync.WaitGroup
		ctx, cancel := context.WithCancel(background)
		var mtx sync.Mutex

		for k, v := range next {
			if v == current[k] {
				log.WithFields(log.Fields{"url": k}).Trace("Not triggering URL")
			} else {
				log.WithFields(log.Fields{"url": k}).Info("Some images changed, triggering URL")

				wg.Add(1)
				go trigger(k, current, next, &mtx, ctx, cancel, &wg)
			}
		}

		wg.Wait()
	}

	log.WithFields(log.Fields{"file": statePath}).Debug("Writing state")

	var buf bytes.Buffer
	if errEG := gob.NewEncoder(&buf).Encode(next); errEG != nil {
		log.WithFields(log.Fields{"file": statePath, "error": jsonableError{errEG}}).Error("Couldn't write state")
		return
	}

	if errWF := fatomic.WriteFile(statePath, &buf); errWF != nil {
		log.WithFields(log.Fields{"file": statePath, "error": jsonableError{errWF}}).Error("Couldn't write state")
	}
}

func normalize(image string) string {
	switch strings.Count(image, "/") {
	case 0:
		image = "library/" + image
		fallthrough
	case 1:
		image = "docker.io/" + image
	}

	if parts := strings.Split(image, "/"); !strings.Contains(parts[len(parts)-1], ":") {
		image += ":latest"
	}

	return image
}

func pull(client *docker.Client, image string, ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()

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

func trigger(url string, current, next state, mtx *sync.Mutex, ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup) {
	defer wg.Done()

	success := false
	defer resetUnlessSuccess(&success, current, next, url, mtx)

	req, errRC := http.NewRequestWithContext(ctx, "POST", url, nil)
	if errRC != nil {
		if errRC != context.Canceled {
			log.WithFields(log.Fields{"url": url, "error": jsonableError{errRC}}).Error("Couldn't trigger URL")
			cancel()
		}

		return
	}

	req.Header.Set("User-Agent", "dockerhub-chainreactor/1717")

	resp, errDR := http.DefaultClient.Do(req)
	if errDR != nil {
		if errDR != context.Canceled {
			log.WithFields(log.Fields{"url": url, "error": jsonableError{errDR}}).Error("Couldn't trigger URL")
			cancel()
		}

		return
	}

	defer resp.Body.Close()

	io.Copy(nullWriter{}, resp.Body)

	if resp.StatusCode > 299 {
		log.WithFields(log.Fields{"url": url, "status": resp.StatusCode}).Warn("Couldn't trigger URL")
		return
	}

	success = true
}

func resetUnlessSuccess(success *bool, current, next state, url string, mtx *sync.Mutex) {
	if !*success {
		mtx.Lock()
		defer mtx.Unlock()

		next[url] = current[url]
	}
}
