package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fastly/cli/pkg/cmd"
	"github.com/fastly/cli/pkg/config"
	"github.com/fastly/cli/pkg/filesystem"
	"github.com/fastly/cli/pkg/revision"
	fstruntime "github.com/fastly/cli/pkg/runtime"
	"github.com/fastly/cli/pkg/text"
)

// RootCommand is the parent command for all subcommands in this package.
// It should be installed under the primary root command.
type RootCommand struct {
	cmd.Base
	cliVersioner   Versioner
	configFilePath string
}

// NewRootCommand returns a new command registered in the parent.
func NewRootCommand(parent cmd.Registerer, configFilePath string, cliVersioner Versioner, globals *config.Data) *RootCommand {
	var c RootCommand
	c.Globals = globals
	c.CmdClause = parent.Command("update", "Update the CLI to the latest version")
	c.cliVersioner = cliVersioner
	c.configFilePath = configFilePath
	return &c
}

// Exec implements the command interface.
func (c *RootCommand) Exec(_ io.Reader, out io.Writer) error {
	current, latest, shouldUpdate := Check(context.Background(), revision.AppVersion, c.cliVersioner)

	text.Break(out)
	text.Output(out, "Current version: %s", current)
	text.Output(out, "Latest version: %s", latest)
	text.Break(out)

	progress := text.NewProgress(out, c.Globals.Verbose())
	progress.Step("Updating versioning information...")

	progress.Step("Checking CLI binary update...")
	if !shouldUpdate {
		text.Output(out, "No update required.")
		return nil
	}

	progress.Step("Fetching latest release...")
	latestPath, err := c.cliVersioner.Download(context.Background(), latest)
	if err != nil {
		c.Globals.ErrLog.AddWithContext(err, map[string]any{
			"Current CLI version": current,
			"Latest CLI version":  latest,
		})
		progress.Fail()
		return fmt.Errorf("error downloading latest release: %w", err)
	}
	defer os.RemoveAll(latestPath)

	progress.Step("Replacing binary...")
	execPath, err := os.Executable()
	if err != nil {
		c.Globals.ErrLog.Add(err)
		progress.Fail()
		return fmt.Errorf("error determining executable path: %w", err)
	}

	currentPath, err := filepath.Abs(execPath)
	if err != nil {
		c.Globals.ErrLog.AddWithContext(err, map[string]any{
			"Executable path": execPath,
		})
		progress.Fail()
		return fmt.Errorf("error determining absolute target path: %w", err)
	}

	// Windows does not permit removing a running executable, however it will
	// permit renaming it! So we first rename the running executable and then we
	// move the executable that we downloaded to the same location as the
	// original executable (which is allowed since we first renamed the running
	// executable).
	//
	// Reference:
	// https://github.com/golang/go/issues/21997#issuecomment-331744930
	if fstruntime.Windows {
		if err := os.Rename(execPath, execPath+"~"); err != nil {
			c.Globals.ErrLog.Add(err)
			if err = os.Remove(execPath + "~"); err != nil {
				c.Globals.ErrLog.Add(err)
			}
		}
	}

	if err := os.Rename(latestPath, currentPath); err != nil {
		if err := filesystem.CopyFile(latestPath, currentPath); err != nil {
			c.Globals.ErrLog.AddWithContext(err, map[string]any{
				"Executable (source)":      latestPath,
				"Executable (destination)": currentPath,
			})
			progress.Fail()

			return fmt.Errorf("error moving latest binary in place: %w", err)
		}
	}

	progress.Done()

	text.Success(out, "Updated %s to %s.", currentPath, latest)
	return nil
}
