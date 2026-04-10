package main

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// initLogging wires up zerolog so every record lands in two places:
//  1. a human-readable ConsoleWriter on stderr (what `docker logs` shows)
//  2. the in-memory ring buffer exposed by the dashboard log panel
//
// Both sinks go through ConsoleWriter (colour disabled for the ring so the
// escape codes don't pollute the HTML <pre>).
func initLogging(s *State) {
	stderrOut := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}
	ringOut := zerolog.ConsoleWriter{
		Out:        &ringWriter{state: s},
		TimeFormat: "15:04:05",
		NoColor:    true,
	}

	multi := zerolog.MultiLevelWriter(stderrOut, ringOut)
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()
}

// ringWriter is an io.Writer that appends each received line into the State
// ring buffer. zerolog's ConsoleWriter hands us one formatted record at a
// time, so we don't need to re-parse anything.
type ringWriter struct {
	state *State
}

func (r *ringWriter) Write(p []byte) (int, error) {
	s := strings.TrimRight(string(p), "\n")
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			r.state.Log(line)
		}
	}
	return len(p), nil
}

// Ensure *ringWriter satisfies io.Writer — used by zerolog under the hood.
var _ io.Writer = (*ringWriter)(nil)
