package main

import (
	"context"
	"crypto/sha1"
	"encoding"
	"fmt"
	"github.com/robfig/cron/v3"
	lev "github.com/schollz/closestmatch/levenshtein"
	log "github.com/sirupsen/logrus"
	"hash"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

const watchPath = "./"
const configPath = "config.yml"
const statePath = "state.gob"

var logLevels = func() *lev.ClosestMatch {
	asStrs := make([]string, 0, len(log.AllLevels))
	for _, lvl := range log.AllLevels {
		asStrs = append(asStrs, lvl.String())
	}

	return lev.New(asStrs)
}()

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
var background = context.Background()

type jsonableError struct {
	err error
}

var _ encoding.TextMarshaler = jsonableError{}

func (je jsonableError) MarshalText() (text []byte, err error) {
	return []byte(je.err.Error()), nil
}

type jsonableStringer struct {
	str fmt.Stringer
}

var _ encoding.TextMarshaler = jsonableStringer{}

func (js jsonableStringer) MarshalText() (text []byte, err error) {
	return []byte(js.str.String()), nil
}

type jsonableBadLogLevelAlt struct {
	badLogLevel string
}

var _ encoding.TextMarshaler = jsonableBadLogLevelAlt{}

func (jblla jsonableBadLogLevelAlt) MarshalText() (text []byte, err error) {
	return []byte(logLevels.Closest(strings.ToLower(jblla.badLogLevel))), nil
}

type nullWriter struct {
}

var _ io.Writer = nullWriter{}

func (nullWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type hashWrapper64 struct {
	wrapped hash.Hash
}

var _ hash.Hash64 = hashWrapper64{}

func (hw hashWrapper64) Write(p []byte) (int, error) {
	return hw.wrapped.Write(p)
}

func (hw hashWrapper64) Sum(b []byte) []byte {
	return hw.wrapped.Sum(b)
}

func (hw hashWrapper64) Reset() {
	hw.wrapped.Reset()
}

func (hw hashWrapper64) Size() int {
	return hw.wrapped.Size()
}

func (hw hashWrapper64) BlockSize() int {
	return hw.wrapped.BlockSize()
}

func (hw hashWrapper64) Sum64() uint64 {
	return 0
}

type hubConfig struct {
	Post string   `yaml:"post"`
	Base []string `yaml:"base"`
}

type configuration struct {
	Log struct {
		Level string `yaml:"level"`
	} `yaml:"log"`
	Build struct {
		Every string `yaml:"every"`
	} `yaml:"build"`
	Hub []hubConfig `yaml:"hub"`
}

type state map[string][sha1.Size]byte

func initLogging() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.TraceLevel)
}

func wait4term() {
	signals := [2]os.Signal{syscall.SIGTERM, syscall.SIGINT}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals[:]...)

	log.WithFields(log.Fields{"signals": signals}).Trace("Listening for signals")

	log.WithFields(log.Fields{"signal": <-ch}).Warn("Terminating")
	os.Exit(0)
}
