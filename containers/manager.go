package containers

import (
	"encoding/json"
	"fmt"
	base "github.com/g8os/core.base"
	"github.com/g8os/core.base/pm"
	"github.com/g8os/core.base/pm/core"
	"github.com/g8os/core.base/pm/process"
	"github.com/g8os/core.base/utils"
	"github.com/g8os/core0/assets"
	"github.com/garyburd/redigo/redis"
	"github.com/op/go-logging"
	"github.com/pborman/uuid"
	"github.com/vishvananda/netlink"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"sync"
	"time"
)

const (
	cmdContainerCreate    = "corex.create"
	cmdContainerList      = "corex.list"
	cmdContainerDispatch  = "corex.dispatch"
	cmdContainerTerminate = "corex.terminate"

	coreXResponseQueue = "corex:results"
	coreXBinaryName    = "coreX"

	redisSocketSrc     = "/var/run/redis.socket"
	zeroTierScriptPath = "/tmp/zerotier.sh"
)

var (
	log = logging.MustGetLogger("containers")
)

type ContainerBridgeSettings [2]string

func (s ContainerBridgeSettings) Name() string {
	return s[0]
}

func (s ContainerBridgeSettings) Setup() string {
	return s[1]
}

func (s ContainerBridgeSettings) String() string {
	return fmt.Sprintf("%v:%v", s[0], s[1])
}

type Network struct {
	ZeroTier string                    `json:"zerotier,omitempty"`
	Bridge   []ContainerBridgeSettings `json:"bridge,omitempty"`
}

type ContainerCreateArguments struct {
	Root    string            `json:"root"`    //Root plist
	Mount   map[string]string `json:"mount"`   //data disk mounts.
	Network Network           `json:"network"` // network setup
	Port    map[int]int       `json:"port"`    //port forwards
}

type ContainerDispatchArguments struct {
	Container uint64       `json:"container"`
	Command   core.Command `json:"command"`
}

func (c *ContainerCreateArguments) Valid() error {
	if c.Root == "" {
		return fmt.Errorf("root plist is required")
	}

	for host, guest := range c.Mount {
		u, err := url.Parse(host)
		if err != nil {
			return fmt.Errorf("invalid host mount: %s", err)
		}
		if u.Scheme != "" {
			//probably a plist
			continue
		}
		p := u.Path
		if !path.IsAbs(p) {
			return fmt.Errorf("host path '%s' must be absolute", host)
		}
		if !path.IsAbs(guest) {
			return fmt.Errorf("guest path '%s' must be absolute", guest)
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return fmt.Errorf("host path '%s' does not exist", p)
		}
	}

	for host, guest := range c.Port {
		if host < 0 || host > 65535 {
			return fmt.Errorf("invalid host port '%d'", host)
		}
		if guest < 0 || guest > 65535 {
			return fmt.Errorf("invalid guest port '%d'", guest)
		}
	}

	for _, bridge := range c.Network.Bridge {
		link, err := netlink.LinkByName(bridge.Name())
		if err != nil {
			return err
		}

		if link.Type() != "bridge" {
			return fmt.Errorf("bridge '%s' doesn't exist", c.Network.Bridge)
		}
	}

	return nil
}

type containerManager struct {
	sequence uint64
	mutex    sync.Mutex

	pool   *redis.Pool
	ensure sync.Once

	sinks map[string]base.SinkClient
}

/*
WARNING:
	Code here assumes that redis-server is started by core0 by the configuration files. If it wasn't started or failed
	to start, commands like core.create, core.dispatch, etc... will fail.
TODO:
	May be make redis-server start part of the bootstrap process without the need to depend on external configuration
	to run it.
*/

func Containers(sinks map[string]base.SinkClient) {
	containerMgr := &containerManager{
		pool:  utils.NewRedisPool("unix", redisSocketSrc, ""),
		sinks: sinks,
	}

	script, err := assets.Asset("scripts/network.sh")
	if err != nil {
		panic(err)
	}

	if err := ioutil.WriteFile(
		zeroTierScriptPath,
		script,
		0754,
	); err != nil {
		panic(err)
	}

	pm.RegisterCmd("zerotier", "bash", "/", []string{zeroTierScriptPath, "{netns}", "{zerotier}"}, nil)

	pm.CmdMap[cmdContainerCreate] = process.NewInternalProcessFactory(containerMgr.create)
	pm.CmdMap[cmdContainerList] = process.NewInternalProcessFactory(containerMgr.list)
	pm.CmdMap[cmdContainerDispatch] = process.NewInternalProcessFactory(containerMgr.dispatch)
	pm.CmdMap[cmdContainerTerminate] = process.NewInternalProcessFactory(containerMgr.terminate)

	go containerMgr.startForwarder()
}

func (m *containerManager) forwardNext() error {
	db := m.pool.Get()
	defer db.Close()

	payload, err := redis.ByteSlices(db.Do("BLPOP", coreXResponseQueue, 0))
	if err != nil {
		return err
	}

	var result core.JobResult
	if err := json.Unmarshal(payload[1], &result); err != nil {
		log.Errorf("Failed to load command: %s", err)
		return nil //no wait.
	}

	//use command tags for routing.
	if sink, ok := m.sinks[result.Tags]; ok {
		log.Debugf("Forwarding job result to %s", result.Tags)
		return sink.Respond(&result)
	} else {
		log.Warningf("Received a corex result for an unknown sink: %s", result.Tags)
	}

	return nil
}

func (m *containerManager) startForwarder() {
	log.Debugf("Start container results forwarder")
	for {
		if err := m.forwardNext(); err != nil {
			log.Warningf("Failed to forward command result: %s", err)
			time.Sleep(2 * time.Second)
		}
	}
}

func (m *containerManager) getNextSequence() uint64 {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sequence += 1
	return m.sequence
}

func (m *containerManager) create(cmd *core.Command) (interface{}, error) {
	var args ContainerCreateArguments
	if err := json.Unmarshal(*cmd.Arguments, &args); err != nil {
		return nil, err
	}

	if err := args.Valid(); err != nil {
		return nil, err
	}

	id := m.getNextSequence()
	c := newContainer(id, cmd.Route, &args)

	if err := c.Start(); err != nil {
		return nil, err
	}

	return id, nil
}

func (m *containerManager) list(cmd *core.Command) (interface{}, error) {
	containers := make(map[uint64]*process.ProcessStats)

	for name, runner := range pm.GetManager().Runners() {
		var id uint64
		if n, err := fmt.Sscanf(name, "core-%d", &id); err != nil || n != 1 {
			continue
		}
		ps := runner.Process()
		if ps != nil {
			state := ps.GetStats()
			state.Cmd = nil
			containers[id] = state
		} else {
			containers[id] = nil
		}
	}

	return containers, nil
}

func (m *containerManager) getCoreXQueue(id uint64) string {
	return fmt.Sprintf("core:%v", id)
}

func (m *containerManager) dispatch(cmd *core.Command) (interface{}, error) {
	var args ContainerDispatchArguments
	if err := json.Unmarshal(*cmd.Arguments, &args); err != nil {
		return nil, err
	}

	if args.Container <= 0 {
		return nil, fmt.Errorf("invalid container id")
	}

	if _, ok := pm.GetManager().Runners()[fmt.Sprintf("core-%d", args.Container)]; !ok {
		return nil, fmt.Errorf("container does not exist")
	}

	id := uuid.New()
	args.Command.ID = id
	args.Command.Tags = string(cmd.Route)

	db := m.pool.Get()
	defer db.Close()

	data, err := json.Marshal(args.Command)
	if err != nil {
		return nil, err
	}

	_, err = db.Do("RPUSH", m.getCoreXQueue(args.Container), string(data))

	return id, err
}

type ContainerTerminateArguments struct {
	Container uint64 `json:"container"`
}

func (m *containerManager) terminate(cmd *core.Command) (interface{}, error) {
	var args ContainerTerminateArguments
	if err := json.Unmarshal(*cmd.Arguments, &args); err != nil {
		return nil, err
	}

	coreID := fmt.Sprintf("core-%d", args.Container)
	pm.GetManager().Kill(coreID)

	return nil, nil
}
