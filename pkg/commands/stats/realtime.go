package stats

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/fastly/cli/pkg/api"
	"github.com/fastly/cli/pkg/cmd"
	"github.com/fastly/cli/pkg/config"
	"github.com/fastly/cli/pkg/manifest"
	"github.com/fastly/cli/pkg/text"
	"github.com/fastly/go-fastly/v6/fastly"
)

// RealtimeCommand exposes the Realtime Metrics API.
type RealtimeCommand struct {
	cmd.Base
	manifest manifest.Data

	formatFlag  string
	serviceName cmd.OptionalServiceNameID
}

// NewRealtimeCommand is the "stats realtime" subcommand.
func NewRealtimeCommand(parent cmd.Registerer, globals *config.Data, data manifest.Data) *RealtimeCommand {
	var c RealtimeCommand
	c.Globals = globals
	c.manifest = data

	c.CmdClause = parent.Command("realtime", "View realtime stats for a Fastly service")
	c.RegisterFlag(cmd.StringFlagOpts{
		Name:        cmd.FlagServiceIDName,
		Description: cmd.FlagServiceIDDesc,
		Dst:         &c.manifest.Flag.ServiceID,
		Short:       's',
	})
	c.RegisterFlag(cmd.StringFlagOpts{
		Action:      c.serviceName.Set,
		Name:        cmd.FlagServiceName,
		Description: cmd.FlagServiceDesc,
		Dst:         &c.serviceName.Value,
	})

	c.CmdClause.Flag("format", "Output format (json)").EnumVar(&c.formatFlag, "json")

	return &c
}

// Exec implements the command interface.
func (c *RealtimeCommand) Exec(_ io.Reader, out io.Writer) error {
	serviceID, source, flag, err := cmd.ServiceID(c.serviceName, c.manifest, c.Globals.APIClient, c.Globals.ErrLog)
	if err != nil {
		return err
	}
	if c.Globals.Verbose() {
		cmd.DisplayServiceID(serviceID, flag, source, out)
	}

	switch c.formatFlag {
	case "json":
		if err := loopJSON(c.Globals.RTSClient, serviceID, out); err != nil {
			c.Globals.ErrLog.AddWithContext(err, map[string]any{
				"Service ID": serviceID,
			})
			return err
		}

	default:
		if err := loopText(c.Globals.RTSClient, serviceID, out); err != nil {
			c.Globals.ErrLog.AddWithContext(err, map[string]any{
				"Service ID": serviceID,
			})
			return err
		}
	}

	return nil
}

func loopJSON(client api.RealtimeStatsInterface, service string, out io.Writer) error {
	var timestamp uint64
	for {
		var envelope struct {
			Timestamp uint64            `json:"timestamp"`
			Data      []json.RawMessage `json:"data"`
		}

		err := client.GetRealtimeStatsJSON(&fastly.GetRealtimeStatsInput{
			ServiceID: service,
			Timestamp: timestamp,
		}, &envelope)
		if err != nil {
			text.Error(out, "fetching stats: %w", err)
			continue
		}
		timestamp = envelope.Timestamp

		for _, data := range envelope.Data {
			_, err = out.Write(data)
			if err != nil {
				return fmt.Errorf("error: unable to write data to stdout: %w", err)
			}
			text.Break(out)
		}
	}
}

func loopText(client api.RealtimeStatsInterface, service string, out io.Writer) error {
	var timestamp uint64
	for {
		var envelope realtimeResponse

		err := client.GetRealtimeStatsJSON(&fastly.GetRealtimeStatsInput{
			ServiceID: service,
			Timestamp: timestamp,
		}, &envelope)
		if err != nil {
			text.Error(out, "fetching stats: %w", err)
			continue
		}
		timestamp = envelope.Timestamp

		for _, block := range envelope.Data {
			agg := block.Aggregated

			// FIXME: These are heavy-handed compatibility
			// fixes for stats vs realtime, so we can use
			// fmtBlock for both.
			agg["start_time"] = block.Recorded
			delete(agg, "miss_histogram")

			if err := fmtBlock(out, service, agg); err != nil {
				text.Error(out, "formatting stats: %w", err)
				continue
			}
		}
	}
}
