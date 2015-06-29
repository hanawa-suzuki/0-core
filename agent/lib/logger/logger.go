package logger

import (
    "log"
    "time"
    "encoding/json"
    "bytes"
    "net/http"
    "github.com/Jumpscale/jsagent/agent/lib/pm"
    "github.com/Jumpscale/jsagent/agent/lib/utils"
)

type Logger interface {
    Log(msg *pm.Message)
}

type DBLogger struct {
    factory DBFactory
    defaults []int
}

func NewDBLogger(factory DBFactory, defaults []int) Logger {
    return &DBLogger{
        factory: factory,
        defaults: defaults,
    }
}

func (logger *DBLogger) Log(msg *pm.Message) {
    levels := logger.defaults
    msgLevels := msg.Cmd.Args.GetIntArray("loglevels_db")

    if len(msgLevels) > 0 {
        levels = msgLevels
    }

    if len(levels) > 0 && !utils.In(levels, msg.Level) {
        return
    }

    db := logger.factory.GetDBCon()
    stmnt := `
        insert into logs (id, domain, name, epoch, level, data)
        values (?, ?, ?, ?, ?, ?)
    `
    args := msg.Cmd.Args
    _, err := db.Exec(stmnt, msg.Id, args.GetString("domain"), args.GetString("name"),
                      msg.Epoch, msg.Level, msg.Message)
    if err != nil {
        log.Fatal(err)
    }
}


type ACLogger struct {
    endpoints []string
    buffer []*pm.Message
    queue chan *pm.Message
    defaults []int
}

func NewACLogger(endpoints []string, bufsize int, flushInt time.Duration, defaults []int) Logger {
    logger := &ACLogger {
        endpoints: endpoints,
        buffer: make([]*pm.Message, 0, bufsize),
        queue: make(chan *pm.Message),
        defaults: defaults,
    }

    go func() {
        //autostart logger flusher.
        for {
            select {
            case msg := <- logger.queue:
                if len(logger.buffer) < cap(logger.buffer) {
                    logger.buffer = append(logger.buffer, msg)
                }

                if len(logger.buffer) == cap(logger.buffer) {
                    //no more buffer space.
                    logger.flush()
                }
            case <- time.After(flushInt):
                logger.flush()
            }
        }
    } ()

    return logger
}

func (logger *ACLogger) Log(msg *pm.Message) {
    levels := logger.defaults
    msgLevels := msg.Cmd.Args.GetIntArray("loglevels_db")

    if len(msgLevels) > 0 {
        levels = msgLevels
    }

    if len(levels) > 0 && !utils.In(levels, msg.Level) {
        return
    }

    logger.queue <- msg
}

func (logger *ACLogger) flush() {
    basket := make([]*pm.Message, len(logger.buffer))
    copy(basket, logger.buffer)
    go logger.send(basket)

    logger.buffer = logger.buffer[0:0]
}

func (logger *ACLogger) send(buffer []*pm.Message) {
    if len(buffer) == 0 {
        //buffer can be of length zero, when flushed on timer while
        //no messages are ready.
        return
    }

    msgs, err := json.Marshal(buffer)
    if err != nil {
        log.Println("Failed to serialize the logs")
        return
    }

    reader := bytes.NewReader(msgs)
    for _, endpoint := range logger.endpoints {
        resp, err := http.Post(endpoint, "application/json", reader)
        if err != nil {
            log.Println("Failed to send log batch to AC", endpoint, err)
            continue
        }
        defer resp.Body.Close()
    }
}

type ConsoleLogger struct {
    defaults []int
}

func NewConsoleLogger(defaults []int) Logger {
    return &ConsoleLogger{
        defaults: defaults,
    }
}

func (logger *ConsoleLogger) Log(msg *pm.Message) {
    if len(logger.defaults) > 0 && !utils.In(logger.defaults, msg.Level) {
        return
    }

    log.Println(msg)
}
