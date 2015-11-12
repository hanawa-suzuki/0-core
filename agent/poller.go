package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Jumpscale/agent2/agent/lib/pm"
	"github.com/Jumpscale/agent2/agent/lib/pm/core"
	"github.com/Jumpscale/agent2/agent/lib/settings"
	"io/ioutil"
	"log"
	"net/url"
	"strings"
	"time"
)

type poller struct {
	key        string
	manager    *pm.PM
	controller *ControllerClient
	config     *settings.Settings
}

func newPoller(key string, manager *pm.PM, controller *ControllerClient, config *settings.Settings) *poller {
	poll := &poller{
		key:        key,
		manager:    manager,
		controller: controller,
		config:     config,
	}

	return poll
}

func (poll *poller) longPoll() {
	lastfail := time.Now().Unix()
	controller := poll.controller
	client := controller.Client
	config := poll.config

	sendStartup := true

	event, _ := json.Marshal(map[string]string{
		"name": "startup",
	})

	pollQuery := make(url.Values)

	for _, role := range config.Main.Roles {
		pollQuery.Add("role", role)
	}

	pollURL := fmt.Sprintf("%s?%s", controller.BuildURL(config.Main.Gid, config.Main.Nid, "cmd"),
		pollQuery.Encode())

	for {
		if sendStartup {
			//this happens on first loop, or if the connection to the controller was gone and then
			//restored.
			reader := bytes.NewBuffer(event)

			url := controller.BuildURL(config.Main.Gid, config.Main.Nid, "event")

			resp, err := client.Post(url, "application/json", reader)
			if err != nil {
				log.Println("Failed to send startup event to AC", url, err)
			} else {
				resp.Body.Close()
				sendStartup = false
			}
		}

		response, err := client.Get(pollURL)
		if err != nil {
			log.Println("No new commands, retrying ...", controller.URL, err)
			//HTTP Timeout
			if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "EOF") {
				//make sure to send startup even on the next try. In case
				//agent controller was down or even booted after the agent.
				sendStartup = true
			}

			if time.Now().Unix()-lastfail < ReconnectSleepTime {
				time.Sleep(ReconnectSleepTime * time.Second)
			}
			lastfail = time.Now().Unix()

			continue
		}

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Println("Failed to load response content", err)
			continue
		}

		response.Body.Close()
		if response.StatusCode != 200 {
			log.Println("Failed to retrieve jobs", response.Status, string(body))
			time.Sleep(2 * time.Second)
			continue
		}

		if len(body) == 0 {
			//no data, can be a long poll timeout
			continue
		}

		cmd, err := core.LoadCmd(body)
		if err != nil {
			log.Println("Failed to load cmd", err, string(body))
			continue
		}

		//set command defaults
		//1 - stats_interval
		meterInt := cmd.Args.GetInt("stats_interval")
		if meterInt == 0 {
			cmd.Args.Set("stats_interval", config.Stats.Interval)
		}

		//tag command for routing.
		ctrlConfig := controller.Config
		cmd.Args.SetTag(poll.key)
		cmd.Args.SetController(ctrlConfig)

		cmd.Gid = config.Main.Gid
		cmd.Nid = config.Main.Nid

		log.Println("Starting command", cmd)

		if cmd.Args.GetString("queue") == "" {
			poll.manager.RunCmd(cmd)
		} else {
			poll.manager.RunCmdQueued(cmd)
		}
	}
}

/*
StartPollers starts the long polling routines and feed the manager with received commands
*/
func StartPollers(manager *pm.PM, controllers map[string]*ControllerClient, config *settings.Settings) {
	var keys []string
	if len(config.Channel.Cmds) > 0 {
		keys = config.Channel.Cmds
	} else {
		keys = getKeys(controllers)
	}

	for _, key := range keys {
		controller, ok := controllers[key]
		if !ok {
			log.Fatalf("No contoller with name '%s'\n", key)
		}

		poll := newPoller(key, manager, controller, config)
		go poll.longPoll()
	}
}