# nomad-crashloop-detector

`nomad-crashloop-detector` is a tool meant to detect allocation crash-loops, by consuming the allocation stream from [nomad-crashloop-detector](https://github.com/seatgeek/nomad-crashloop-detector) in RabbitMQ

## Running

The project got build artifacts for linux, darwin and windows in the [GitHub releases tab](https://github.com/seatgeek/nomad-crashloop-detector/releases).

A docker container is also provided at [seatgeek/nomad-crashloop-detector](https://hub.docker.com/r/seatgeek/nomad-crashloop-detector/tags/)

## Requirements

- Go 1.8

## Building

To build a binary, run the following

```shell
# get this repo
go get github.com/seatgeek/nomad-crashloop-detector

# go to the repo directory
cd $GOPATH/src/github.com/seatgeek/nomad-crashloop-detector

# build the `nomad-crashloop-detector` binary
make build
```

This will create a `nomad-crashloop-detector` binary in your `$GOPATH/bin` directory.

## Configuration

Any `NOMAD_*` env that the native `nomad` CLI tool supports are supported by this tool.

- `$AMQP_CONNECTION` is identical to `$SINK_AMQP_CONNECTION`, but is for the consuming stream from `nomad-firehose`
- `$AMQP_QUEUE` is the RabbitMQ queue to consume the `nomad-firehose` from.
- `$RESTART_COUNT` how many restarts to allow within `$RESTART_INTERVAL` time (example: `5`)
- `$RESTART_INTERVAL` within what time frame `$RESTART_COUNT` allocation restarts must happen to trigger an notification (example: `5m`)
- `$NOTIFICATION_INTERVAL` how often a notification should happen on a crash-looping allocation (example: `5m`)

## Sinks

The sink type is configured using `$SINK_TYPE` environment variable. Valid values are: `stdout`, `kinesis` and `amqp`.

The `amqp` sink is configured using `$SINK_AMQP_CONNECTION` (`amqp://guest:guest@127.0.0.1:5672/`), `$SINK_AMQP_EXCHANGE` and `$SINK_AMQP_ROUTING_KEY` environment variables.

The `kinesis` sink is configured using `$SINK_KINESIS_STREAM_NAME` and `$SINK_KINESIS_PARTITION_KEY` environment variables.

The `stdout` sink do not have any configuration, it will simply output the JSON to stdout for debugging.

## Example

Assuming the following setup:

- `nomad` exchange (type=topic)
- `nomad.crash-loop-in` queue which is bound to `nomad` exchange with routing key `allocations`
- `nomad.crash-loop-out` queue which is bound to `nomad` exchange with routing key `crash-loop`

Running `nomad-firehose`:

```sh
SINK_TYPE=amqp \
SINK_AMQP_CONNECTION="amqp://guest:guest@127.0.0.1:5672/" \
SINK_AMQP_EXCHANGE=nomad \
SINK_AMQP_ROUTING_KEY=allocations \
nomad-firehose allocations
```

Running `nomad-crashloop-detector`:

```sh
RESTART_COUNT=2 \
RESTART_INTERVAL=5m \
NOTIFICATION_INTERVAL=5m \
SINK_TYPE=amqp \
SINK_AMQP_CONNECTION="amqp://guest:guest@127.0.0.1:5672/" \
SINK_AMQP_EXCHANGE=nomad \
SINK_AMQP_ROUTING_KEY=crash-loop \
AMQP_CONNECTION=$SINK_AMQP_CONNECTION \
AMQP_QUEUE=nomad.crash-loop-in \
nomad-crashloop-detector
```

The setup will make `nomad-firehose` send all nomad allocation changes to the `nomad` exchange, that will forward messages to the `nomad.crash-loop-in` queue.
`nomad-crashloop-detector` will consume the messages in `nomad.crash-loop-in`, and when a restart threshold is reached, submit a AMQP job to the `nomad` exchange, which will redirect the message to `nomad.crash-loop-in`.

## Example crash-loop payload

```json
{
    "LastEvent": {
        "Name": "job.task[0]",
        "AllocationID": "fd4deb1f-405b-93a6-3eb4-a84e0670049d",
        "DesiredStatus": "run",
        "DesiredDescription": "",
        "ClientStatus": "running",
        "ClientDescription": "",
        "JobID": "job",
        "GroupName": "group",
        "TaskName": "task",
        "EvalID": "db0064ab-a44d-e450-4f66-2cabbec536bb",
        "TaskState": "pending",
        "TaskFailed": false,
        "TaskStartedAt": "2017-07-12T13:56:30.932498912Z",
        "TaskFinishedAt": "0001-01-01T00:00:00Z",
        "TaskEvent": {
            "Type": "Restarting",
            "Time": 1499867806677609000,
            "FailsTask": false,
            "RestartReason": "Restart within policy",
            "SetupError": "",
            "DriverError": "",
            "DriverMessage": "",
            "ExitCode": 0,
            "Signal": 0,
            "Message": "",
            "KillReason": "",
            "KillTimeout": 0,
            "KillError": "",
            "StartDelay": 17425840945,
            "DownloadError": "",
            "ValidationError": "",
            "DiskLimit": 0,
            "DiskSize": 0,
            "FailedSibling": "",
            "VaultError": "",
            "TaskSignalReason": "",
            "TaskSignal": ""
        }
    },
    "EventLog": [
        "2017-07-12T15:56:15.401013209+02:00",
        "2017-07-12T15:56:46.677608921+02:00"
    ]
}
```
