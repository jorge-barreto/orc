package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/ux"
	cli "github.com/urfave/cli/v3"
)

func flowCmd() *cli.Command {
	return &cli.Command{
		Name:  "flow",
		Usage: "Visualize the workflow as a flow diagram",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}
			configPath := filepath.Join(projectRoot, ".orc", "config.yaml")
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			ux.FlowViz(cfg)
			return nil
		},
	}
}
