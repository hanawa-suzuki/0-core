package builtin

import (
    "time"
    "github.com/Jumpscale/jsagent/agent/lib/pm"
)

const (
    CMD_PING = "ping"
)

func init() {
    pm.CMD_MAP[CMD_PING] = InternalProcessFactory(ping)
}

func ping(cmd *pm.Cmd, cfg pm.RunCfg) {
    result := &pm.JobResult {
        Id: cmd.Id,
        Gid: cmd.Gid,
        Nid: cmd.Nid,
        Args: cmd.Args,
        StartTime: time.Now().Unix(),
        Time: 0,
        State: "SUCCESS",
        Level: pm.L_RESULT_JSON,
        Data: `"pong"`,
    }

    cfg.ResultHandler(result)
}