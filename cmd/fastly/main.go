package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/fastly/cli/pkg/app"
	"github.com/fastly/cli/pkg/check"
	"github.com/fastly/cli/pkg/common"
	"github.com/fastly/cli/pkg/config"
	"github.com/fastly/cli/pkg/errors"
	"github.com/fastly/cli/pkg/text"
	"github.com/fastly/cli/pkg/update"
)

func main() {
	// Some configuration options can come from env vars.
	var env config.Environment
	env.Read(parseEnv(os.Environ()))

	// All of the work of building the set of commands and subcommands, wiring
	// them together, picking which one to call, and executing it, occurs in a
	// helper function, Run. We parameterize all of the dependencies so we can
	// test it more easily. Here, we declare all of the dependencies, using
	// the "real" versions that pull e.g. actual commandline arguments, the
	// user's real environment, etc.
	var (
		args                     = os.Args[1:]
		configFilePath           = config.FilePath // write-only for `fastly configure`
		clientFactory            = app.FastlyAPIClient
		httpClient               = http.DefaultClient
		versioner                = update.NewGitHub(context.Background())
		in             io.Reader = os.Stdin
		out            io.Writer = common.NewSyncWriter(os.Stdout)
	)

	// We have to manually handle the inclusion of the verbose flag here because
	// Kingpin doesn't evaluate the provided arguments until app.Run which
	// happens later in the file and yet we need to know if we should be printing
	// output related to the application configuration file in this file.
	var verboseOutput bool
	for _, seg := range args {
		if seg == "-v" || seg == "--verbose" {
			verboseOutput = true
		}
	}

	// Extract a subset of configuration options from the local application directory.
	var file config.File
	err := file.Read(configFilePath)
	if err != nil {
		if verboseOutput {
			if err == config.ErrLegacyConfig {
				text.Output(out, `
					Found your local configuration file (required to use the CLI) was using a legacy format.
					File is being upgraded now.
				`)
			} else {
				text.Output(out, `
					Unable to locate a local configuration file (required to use the CLI).
					File is being created now.
				`)
			}
			text.Break(out)
		}

		err := file.Load(config.RemoteEndpoint, httpClient)
		if err != nil {
			errors.RemediationError{
				Inner:       err,
				Remediation: errors.NetworkRemediation,
			}.Print(os.Stderr)
			os.Exit(1)
		}
	}

	// We have seen a situation where loading data from the remote
	// config endpoint has caused a user to end up with a config in the
	// non-legacy format but with empty values.
	//
	// It's unclear how this happens and so as a temporary measure we'll check if
	// the in-memory data structure is missing a specific value that's set by the
	// CLI, and if so we'll know something bad has happened because at this point
	// we expect the data structure to have a non-empty string value.
	//
	// If we discover we're in that scenario we'll attempt to re-load the
	// configuration from the remote endpoint.
	if file.CLI.LastChecked == "" {
		if verboseOutput {
			text.Warning(out, `
				There was a problem loading the compatibility and versioning information for the Fastly CLI.
				The operation will be retried as this configuration is required.
			`)
			text.Break(out)
		}

		err := file.Load(config.RemoteEndpoint, httpClient)
		if err != nil {
			errors.RemediationError{
				Inner:       err,
				Remediation: errors.NetworkRemediation,
			}.Print(os.Stderr)
			os.Exit(1)
		}
	}

	// When the local configuration file is stale we'll need to acquire the
	// latest version and write that back to disk. To ensure the CLI program
	// doesn't complete before the write has finished, we block via a channel.
	waitForWrite := make(chan bool)
	wait := false

	var errLoadConfig error

	// Validate if configuration is older than its TTL
	if check.Stale(file.CLI.LastChecked, file.CLI.TTL) {
		if verboseOutput {
			text.Info(out, `
Compatibility and versioning information for the Fastly CLI is being updated in the background.  The updated data will be used next time you execute a fastly command.
			`)
		}

		wait = true
		go func() {
			// NOTE: we no longer use the hardcoded config.RemoteEndpoint constant.
			// Instead we rely on the values inside of the application
			// configuration file to determine where to load the config from.
			err := file.Load(file.CLI.RemoteConfig, httpClient)
			if err != nil {
				errLoadConfig = errors.RemediationError{
					Inner:       fmt.Errorf("there was a problem updating the versioning information for the Fastly CLI:\n\n%w", err),
					Remediation: errors.BugRemediation,
				}
			}

			waitForWrite <- true
		}()
	}

	// Main is basically just a shim to call Run, so we do that here.
	if err := app.Run(args, env, file, configFilePath, clientFactory, httpClient, versioner, in, out); err != nil {
		errors.Deduce(err).Print(os.Stderr)

		// NOTE: if we have an error processing the command, then we should be sure
		// to wait for the async file write to complete (otherwise we'll end up in
		// a situation where there is a local application configuration file but
		// with incomplete contents).
		//
		// It would have been nice to just do something like...
		//
		// if wait {
		//   defer func(){
		//     <-waitForWrite
		//     afterWrite(verboseOutput, errLoadConfig, out)
		//   }()
		// }
		//
		// ...and to have this a bit further up the script, as it would have meant
		// we could avoid duplicating the following if statement in two places.
		//
		// As it is, we have to wait for the async write operation here and also at
		// the end of the main function.
		//
		// The problem with defer is that it doesn't work when os.Exit() is
		// encountered, so you either use something like runtime.Goexit() which is
		// pretty hairy and requires other changes like `defer os.Exit(0)` at the
		// top of the main() function (it also has some funky side-effects related
		// to how any other goroutines will persist and errors within those could
		// cause other unexpected behaviour). The alternative is we re-architecture
		// the entire call flow which isn't ideal either.
		//
		// So we've opted for duplication.
		//
		if wait {
			<-waitForWrite
			afterWrite(verboseOutput, errLoadConfig, out)
		}

		os.Exit(1)
	}

	// If the command being run finishes before the latest config is written back
	// to disk, then wait for the write operation to complete.
	//
	// I use a variable instead of calling check.Stale() again, incase the file
	// object has indeed been updated already and is no longer considered stale!
	if wait {
		<-waitForWrite
		afterWrite(verboseOutput, errLoadConfig, out)
	}
}

// afterWrite determines what to do once our waitForWrite channel has received
// a message. The message indicates either the file was written successfully or
// that an error had occurred and so we should display an error message.
func afterWrite(verboseOutput bool, errLoadConfig error, out io.Writer) {
	if verboseOutput && errLoadConfig == nil {
		text.Info(out, config.UpdateSuccessful)
	}
	if errLoadConfig != nil {
		errLoadConfig.(errors.RemediationError).Print(os.Stderr)
	}
}

func parseEnv(environ []string) map[string]string {
	env := map[string]string{}
	for _, kv := range environ {
		toks := strings.SplitN(kv, "=", 2)
		if len(toks) != 2 {
			continue
		}
		k, v := toks[0], toks[1]
		env[k] = v
	}
	return env
}
