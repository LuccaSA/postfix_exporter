// +build !nosystemd,linux

package main

import (
	"context"
	"fmt"
	"log"
	"time"
	"bytes"

	"github.com/alecthomas/kingpin"
	"github.com/coreos/go-systemd/v22/sdjournal"
)

// A SystemdLogSource reads log records from the given Systemd
// journal.
type SystemdLogSource struct {
	journal *sdjournal.JournalReader
	path    string
}

// NewSystemdLogSource returns a log source for reading Systemd
// journal entries. `unit` and `slice` provide filtering if non-empty
// (with `slice` taking precedence).
func NewSystemdLogSource(path, unit, slice string) (*SystemdLogSource, error) {
	var matches []sdjournal.Match
	if slice != "" {
		matches = append(matches, sdjournal.Match{
			Field: sdjournal.SD_JOURNAL_FIELD_SYSTEMD_SLICE,
			Value: slice,
		})
	} else if unit != "" {
		matches = append(matches, sdjournal.Match{
			Field: sdjournal.SD_JOURNAL_FIELD_SYSTEMD_UNIT,
			Value: unit,
		})
	}

	config := sdjournal.JournalReaderConfig{
		NumFromTail: 1,
		Matches: matches,
		Path: path,
		Formatter: fullMessageFormatter,
	}

	j, err := sdjournal.NewJournalReader(config)
	if err != nil {
		return nil, err
	}

	return &SystemdLogSource{journal: j, path: path}, nil
}

// fullMessageFormatter is a formatter for journalreader
// It returns a string representing the current journal entry
// includes the entry timestamp, hostname, syslog_identifier, pid
// and MESSAGE field.
func fullMessageFormatter(entry *sdjournal.JournalEntry) (string, error) {
	msg, ok := entry.Fields["MESSAGE"]
	if !ok {
		return "", fmt.Errorf("no MESSAGE field present in journal entry")
	}
	host, ok := entry.Fields["_HOSTNAME"]
	if !ok {
		return "", fmt.Errorf("no HOSTNAME field present in journal entry")
	}
	id, ok := entry.Fields["SYSLOG_IDENTIFIER"]
	if !ok {
		return "", fmt.Errorf("no SYSLOG_IDENTIFIER field present in journal entry")
	}
	pid, ok := entry.Fields["_PID"]
	if !ok {
		return "", fmt.Errorf("no PID field present in journal entry")
	}

	usec := entry.RealtimeTimestamp
	timestamp := time.Unix(0, int64(usec)*int64(time.Microsecond))

	return fmt.Sprintf("%s %s %s[%s]: %s", timestamp, host, id, pid, msg), nil
}

func (s *SystemdLogSource) Close() error {
	return s.journal.Close()
}

func (s *SystemdLogSource) Path() string {
	return s.path
}

func (s *SystemdLogSource) Read(ctx context.Context) (string, error) {
	const BUFFER_LEN = 255

	var err error
	var msg bytes.Buffer
	var b = make([]byte, BUFFER_LEN)

	for {
		n, err := s.journal.Read(b)
		if err != nil || n == 0 {
			break
		}

		msg.Write(b)

		// A whole entry has been read
		if n < BUFFER_LEN || b[n-1] == '\n' {
			break
		}
	}

	return msg.String(), err
}

// A systemdLogSourceFactory is a factory that can create
// SystemdLogSources from command line flags.
type systemdLogSourceFactory struct {
	enable            bool
	unit, slice, path string
}

func (f *systemdLogSourceFactory) Init(app *kingpin.Application) {
	app.Flag("systemd.enable", "Read from the systemd journal instead of log").Default("false").BoolVar(&f.enable)
	app.Flag("systemd.unit", "Name of the Postfix systemd unit.").Default("postfix.service").StringVar(&f.unit)
	app.Flag("systemd.slice", "Name of the Postfix systemd slice. Overrides the systemd unit.").Default("").StringVar(&f.slice)
	app.Flag("systemd.journal_path", "Path to the systemd journal").Default("").StringVar(&f.path)
}

func (f *systemdLogSourceFactory) New(ctx context.Context) (LogSourceCloser, error) {
	if !f.enable {
		return nil, nil
	}

	log.Println("Reading log events from systemd")
	return NewSystemdLogSource(f.path, f.unit, f.slice)
}

func init() {
	RegisterLogSourceFactory(&systemdLogSourceFactory{})
}
