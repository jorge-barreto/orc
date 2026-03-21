package main

import (
	"context"
	"fmt"
	"os"
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

			flagWorkflow := cmd.Root().String("workflow")
			workflows := discoverWorkflows(projectRoot)

			// If -w specified or single-config, show one workflow
			if flagWorkflow != "" || len(workflows) == 0 {
				_, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
				if err != nil {
					return err
				}
				cfg, err := config.Load(configPath, projectRoot)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				ux.FlowViz(cfg)
				return nil
			}

			// Multi-workflow: show each workflow
			hasConfig := fileExists(filepath.Join(projectRoot, ".orc", "config.yaml"))
			if hasConfig {
				cfg, err := config.Load(filepath.Join(projectRoot, ".orc", "config.yaml"), projectRoot)
				if err == nil {
					fmt.Printf("\n%s═══ Workflow: default (config.yaml) ═══%s\n\n", ux.Bold, ux.Reset)
					ux.FlowViz(cfg)
				} else {
					fmt.Fprintf(os.Stderr, "warning: default (config.yaml): %v\n", err)
				}
			}
			for _, name := range workflows {
				path := filepath.Join(projectRoot, ".orc", "workflows", name+".yaml")
				if !fileExists(path) {
					path = filepath.Join(projectRoot, ".orc", "workflows", name+".yml")
				}
				cfg, err := config.Load(path, projectRoot)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: %s: %v\n", name, err)
					continue
				}
				fmt.Printf("\n%s═══ Workflow: %s ═══%s\n\n", ux.Bold, name, ux.Reset)
				ux.FlowViz(cfg)
			}
			return nil
		},
	}
}
