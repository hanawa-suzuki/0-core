package transport

import (
	"fmt"
	"github.com/garyburd/redigo/redis"
	"github.com/siddontang/ledisdb/config"
	"github.com/siddontang/ledisdb/ledis"
	"github.com/siddontang/ledisdb/server"
	"github.com/zero-os/0-core/base/pm"
	"github.com/zero-os/0-core/core0/assets"
	"github.com/zero-os/0-core/core0/options"
	"sync"
	"time"
)

const (
	SinkQueue = "core:default"
	DBIndex   = 0
)

type Sink struct {
	ch     *channel
	server *server.App
	db     *ledis.DB

	l sync.RWMutex
}

type SinkConfig struct {
	Port int
}

func (c *SinkConfig) Local() string {
	return fmt.Sprintf("127.0.0.1:%d", c.Port)
}

func NewSink(c SinkConfig) (*Sink, error) {
	cfg := config.NewConfigDefault()
	cfg.DBName = "memory"
	cfg.DataDir = "/var/core0"
	cfg.Addr = fmt.Sprintf(":%d", c.Port)
	if orgs, ok := options.Options.Kernel.Get("organization"); ok {
		org := orgs[len(orgs)-1]
		auth, err := AuthMethod(org, string(assets.MustAsset("text/itsyouonline.pub")))
		if err != nil {
			return nil, err
		}
		cfg.AuthMethod = auth
	}

	crt, key, err := generateCRT()
	if err != nil {
		return nil, err
	}

	cfg.TLS = config.TLS{
		Enabled:     true,
		Certificate: crt,
		Key:         key,
	}

	server, err := server.NewApp(cfg)
	if err != nil {
		return nil, err
	}

	db, err := server.Ledis().Select(DBIndex)
	if err != nil {
		return nil, err
	}

	sink := &Sink{
		server: server,
		db:     db,
		ch:     newChannel(db),
	}

	pm.AddHandle(sink)

	return sink, nil
}

func (sink *Sink) RPush(key []byte, args ...[]byte) (int64, error) {
	sink.l.RLock()
	defer sink.l.RUnlock()
	return sink.db.RPush(key, args...)
}

func (sink *Sink) LTrim(key []byte, start, stop int64) error {
	sink.l.RLock()
	defer sink.l.RUnlock()
	return sink.db.LTrim(key, start, stop)
}

func (sink *Sink) Get(key []byte) ([]byte, error) {
	sink.l.RLock()
	defer sink.l.RUnlock()
	return sink.db.Get(key)
}

func (sink *Sink) Set(key []byte, value []byte) error {
	sink.l.RLock()
	defer sink.l.RUnlock()
	return sink.db.Set(key, value)
}

func (sink *Sink) Del(keys ...[]byte) (int64, error) {
	sink.l.RLock()
	defer sink.l.RUnlock()
	return sink.db.Del(keys...)
}

func (sink *Sink) LExpire(key []byte, duration int64) (int64, error) {
	sink.l.RLock()
	defer sink.l.RUnlock()
	return sink.db.LExpire(key, duration)
}

//ResultHandler implementation
func (sink *Sink) Result(cmd *pm.Command, result *pm.JobResult) {
	if err := sink.Forward(result); err != nil {
		log.Errorf("failed to forward result: %s", cmd.ID)
	}
}

func (sink *Sink) process() {

	for {
		var command pm.Command
		err := sink.ch.GetNext(SinkQueue, &command)
		if err == redis.ErrNil {
			continue
		} else if err != nil {
			log.Errorf("Failed to get next command from (%s): %s", SinkQueue, err)
			<-time.After(200 * time.Millisecond)
			continue
		}

		if command.ID == "" {
			log.Warningf("receiving a command with no ID, dropping")
			continue
		}

		sink.ch.Flag(command.ID)
		log.Debugf("Starting command %s", &command)

		_, err = pm.Run(&command)

		if err == pm.UnknownCommandErr {
			result := pm.NewJobResult(&command)
			result.State = pm.StateUnknownCmd
			sink.Forward(result)
		} else if err != nil {
			log.Errorf("Unknown error while processing command (%s): %s", command, err)
		}
	}
}

func (sink *Sink) Forward(result *pm.JobResult) error {
	if result.State != pm.StateDuplicateID {
		/*
			Client tried to push a command with a duplicate id, it means another job
			is running with that ID so we shouldn't flag
		*/
		sink.ch.UnFlag(result.ID)
	}
	return sink.ch.Respond(result)
}

func (sink *Sink) Flag(id string) error {
	return sink.ch.Flag(id)
}

func (sink *Sink) Start() {
	go sink.server.Run()
	go sink.process()
}

func (sink *Sink) GetResult(job string, timeout int) (*pm.JobResult, error) {
	if sink.ch.Flagged(job) {
		return sink.ch.GetResponse(job, timeout)
	} else {
		return nil, fmt.Errorf("unknown job id '%s' (may be it has expired)", job)
	}
}
