package main

import (
	"github.com/fsnotify/fsnotify"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"path"
	"strings"
	"time"
)

func main() {
	initLogging()
	go wait4term()

	watcher := mkWatcher()

LoadConfig:
	for {
		var config *configuration
		var ok bool
		var level log.Level
		var schedule cron.Schedule
		var nextBuild time.Time
		var timer *time.Timer = nil
		var timerCh <-chan time.Time = nil
		var hub []hubConfig

		{
			if config, ok = loadConfig(); ok {
				if config.Log.Level == "" {
					config.Log.Level = "info"
				}

				{
					var errPL error
					if level, errPL = log.ParseLevel(config.Log.Level); errPL != nil {
						log.WithFields(log.Fields{
							"bad_level": config.Log.Level, "did_you_mean": jsonableBadLogLevelAlt{config.Log.Level},
						}).Error("Bad log level")

						ok = false
					}
				}

				if strings.TrimSpace(config.Build.Every) == "" {
					log.Error("Build schedule missing")
					ok = false
				} else {
					var errCP error
					if schedule, errCP = cronParser.Parse(config.Build.Every); errCP != nil {
						log.WithFields(log.Fields{
							"bad_schedule": config.Build.Every, "error": jsonableError{errCP},
						}).Error("Bad build schedule")
						ok = false
					}
				}

				for _, hub := range config.Hub {
					if strings.TrimSpace(hub.Post) == "" {
						log.Error("Trigger URL missing")
						ok = false
					}
				}

				if ok {
					hub = config.Hub
				}
			}
		}

		if ok {
			log.WithFields(log.Fields{"old": log.GetLevel(), "new": level}).Trace("Changing log level")
			log.SetLevel(level)

			now := time.Now()
			nextBuild = schedule.Next(now)

			log.WithFields(log.Fields{"next_build": nextBuild}).Info("Scheduling next build")
			timer, timerCh = prepareSleep(nextBuild.Sub(now))
		}

		for {
			select {
			case now := <-timerCh:
				if now.Before(nextBuild) {
					timer, timerCh = prepareSleep(nextBuild.Sub(now))
				} else {
					log.Info("Building")
					build(hub)

					nextBuild = schedule.Next(time.Now())

					log.WithFields(log.Fields{"next_build": nextBuild}).Info("Scheduling next build")
					timer, timerCh = prepareSleep(nextBuild.Sub(now))
				}
			case event := <-watcher.Events:
				log.WithFields(log.Fields{
					"parent": watchPath, "child": event.Name, "op": jsonableStringer{event.Op},
				}).Trace("Got FS event")

				if event.Op&^fsnotify.Chmod != 0 && path.Clean(event.Name) == configPath {
					if timer != nil {
						timer.Stop()
					}

					continue LoadConfig
				}
			case errWa := <-watcher.Errors:
				log.WithFields(log.Fields{"error": jsonableError{errWa}}).Fatal("FS watcher error")
			}
		}
	}
}

func mkWatcher() *fsnotify.Watcher {
	log.Trace("Setting up FS watcher")

	watcher, errNW := fsnotify.NewWatcher()
	if errNW != nil {
		log.WithFields(log.Fields{"error": jsonableError{errNW}}).Fatal("Couldn't set up FS watcher")
	}

	log.WithFields(log.Fields{"path": watchPath}).Debug("Watching FS")

	if errWA := watcher.Add(watchPath); errWA != nil {
		log.WithFields(log.Fields{"path": watchPath, "error": jsonableError{errWA}}).Fatal("Couldn't watch FS")
	}

	return watcher
}

func loadConfig() (config *configuration, ok bool) {
	log.WithFields(log.Fields{"path": configPath}).Info("Loading config")

	raw, errRF := ioutil.ReadFile(configPath)
	if errRF != nil {
		log.WithFields(log.Fields{"path": configPath, "error": jsonableError{errRF}}).Error("Couldn't read config")
		return
	}

	config = &configuration{}
	if errYU := yaml.Unmarshal(raw, config); errYU != nil {
		log.WithFields(log.Fields{"path": configPath, "error": jsonableError{errYU}}).Error("Couldn't parse config")
		return
	}

	ok = true
	return
}

func prepareSleep(duration time.Duration) (*time.Timer, <-chan time.Time) {
	log.WithFields(log.Fields{"ns": duration}).Trace("Sleeping")

	timer := time.NewTimer(duration)
	return timer, timer.C
}
