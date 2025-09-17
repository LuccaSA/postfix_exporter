package main

import (
	"context"
	"fmt"
	"io"

	"github.com/alecthomas/kingpin"
)

// A LogSourceFactory provides a repository of log sources that can be
// instantiated from command line flags.
type LogSourceFactory interface {
	// Init adds the factory's struct fields as flags in the
	// application.
	Init(*kingpin.Application)

	// New attempts to create a new log source. This is called after
	// flags have been parsed. Returning `nil, nil`, means the user
	// didn't want this log source.
	New(context.Context) (LogSourceCloser, error)
}

type LogSourceCloser interface {
	io.Closer
	LogSource
}

var logSourceFactories []LogSourceFactory

// RegisterLogSourceFactory can be called from module `init` functions
// to register factories.
func RegisterLogSourceFactory(lsf LogSourceFactory) {
	logSourceFactories = append(logSourceFactories, lsf)
}

// InitLogSourceFactories runs Init on all factories. The
// initialization order is arbitrary, except `fileLogSourceFactory` is
// always last (the fallback). The file log source must be last since
// it's enabled by default.
func InitLogSourceFactories(app *kingpin.Application) {
	RegisterLogSourceFactory(&fileLogSourceFactory{})

	for _, f := range logSourceFactories {
		f.Init(app)
	}
}

// NewLogSourceFromFactories iterates through the factories and
// attempts to instantiate a log source. Non-file sources take priority
// over file sources. The first non-file factory to return success wins,
// otherwise falls back to file sources.
func NewLogSourceFromFactories(ctx context.Context) (LogSourceCloser, error) {
	var fileSource LogSourceCloser
	var fileError error

	// First pass: try all non-file sources
	for _, f := range logSourceFactories {
		// Skip file log source factory in first pass
		if _, isFileFactory := f.(*fileLogSourceFactory); isFileFactory {
			// Store the file source for potential fallback
			src, err := f.New(ctx)
			if err != nil {
				fileError = err
			} else {
				fileSource = src
			}
			continue
		}

		src, err := f.New(ctx)
		if err != nil {
			return nil, err
		}
		if src != nil {
			return src, nil
		}
	}

	// If no non-file source was configured, fall back to file source
	if fileError != nil {
		return nil, fileError
	}
	if fileSource != nil {
		return fileSource, nil
	}

	return nil, fmt.Errorf("no log source configured")
}
