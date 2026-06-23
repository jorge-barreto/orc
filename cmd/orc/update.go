package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/jorge-barreto/orc/internal/selfupdate"
	cli "github.com/urfave/cli/v3"
)

func updateCmd() *cli.Command {
	return &cli.Command{
		Name:    "update",
		Aliases: []string{"upgrade"},
		Usage:   "Update orc to the latest release",
		Description: "Downloads the latest released orc binary, verifies its checksum, and " +
			"replaces the running binary in place. Use --check to only report whether an " +
			"update is available. Binaries installed via Homebrew or 'go install' are not " +
			"replaced — the matching package-manager command is suggested instead.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "check", Usage: "Report whether an update is available without installing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runUpdate(ctx, cmd.Bool("check"))
		},
	}
}

func runUpdate(ctx context.Context, checkOnly bool) error {
	current := currentVersion()

	latest, err := selfupdate.ResolveLatest(ctx)
	if err != nil {
		return err
	}

	cmp := selfupdate.CompareVersions(latest, current)
	if checkOnly {
		switch {
		case current == "dev":
			fmt.Printf("current: dev build\nlatest:  %s\n", latest)
		case cmp <= 0:
			fmt.Printf("orc is up to date (%s)\n", current)
		default:
			fmt.Printf("current: %s\nlatest:  %s  (update available)\nrun 'orc update' to install\n", current, latest)
		}
		return nil
	}

	if current != "dev" && cmp <= 0 {
		fmt.Printf("orc is already up to date (%s)\n", current)
		return nil
	}

	// Resolve the real executable path and check how it was installed.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		realExe = exe // fall back to the unresolved path
	}
	switch selfupdate.DetectInstall(realExe) {
	case selfupdate.MethodHomebrew:
		fmt.Printf("orc was installed via Homebrew. Update it with:\n  brew upgrade orc\n")
		return nil
	case selfupdate.MethodGo:
		fmt.Printf("orc was installed via 'go install'. Update it with:\n  go install github.com/jorge-barreto/orc/cmd/orc@latest\n")
		return nil
	}

	fmt.Printf("Updating orc %s -> %s ...\n", current, latest)
	binPath, err := selfupdate.Download(ctx, latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	defer os.Remove(binPath)

	if err := selfupdate.ReplaceBinary(realExe, binPath); err != nil {
		return err
	}
	fmt.Printf("Updated orc to %s\n", latest)
	return nil
}

// currentVersion returns the resolved running version (e.g. "v0.2.0" or "dev"),
// using the same ldflags/BuildInfo resolution as `orc --version`.
func currentVersion() string {
	if version != "dev" {
		return version
	}
	// Mirror versionString()'s BuildInfo fallback for go-install builds.
	return resolveVersion(buildInfoVersion())
}

func buildInfoVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return ""
}
