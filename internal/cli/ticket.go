package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

func newTicketCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "ticket", Short: "Manage tickets and claims"}
	var epicID, typ, title, body, agent, lease, evidence, reason, by, discoveredFrom, testStep string
	var next bool

	list := &cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListTickets(context.Background(), epicID)
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatTickets(emptyTickets(list)))
			}
			return app.Printer.Success(emptyTickets(list))
		},
	}
	list.Flags().StringVar(&epicID, "epic", "", "epic id")
	cmd.AddCommand(list)

	ready := &cobra.Command{
		Use: "ready", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ReadyTickets(context.Background(), epicID)
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatTickets(emptyTickets(list)))
			}
			return app.Printer.Success(emptyTickets(list))
		},
	}
	ready.Flags().StringVar(&epicID, "epic", "", "epic id")
	cmd.AddCommand(ready)

	cmd.AddCommand(&cobra.Command{
		Use: "view <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			t, err := app.DB.GetTicket(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(t)
		},
	})

	add := &cobra.Command{
		Use: "add", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if epicID == "" || title == "" {
				return app.Printer.Fail("missing_fields", "--epic and --title required", "")
			}
			if app.Flags.DryRun {
				return app.dryRun("ticket.add", map[string]any{"epic": epicID, "type": typ, "title": title, "body": body})
			}
			if replayed, err := app.idempotencyReplay(context.Background()); err != nil || replayed {
				return err
			}
			t, err := app.DB.CreateTicketWithMeta(context.Background(), epicID, typ, title, body, nil, discoveredFrom, testStep)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), t); err != nil {
				return err
			}
			return app.Printer.Success(t)
		},
	}
	add.Flags().StringVar(&epicID, "epic", "", "epic id")
	add.Flags().StringVar(&typ, "type", "feature", "ticket type")
	add.Flags().StringVar(&title, "title", "", "title")
	add.Flags().StringVar(&body, "body", "", "body")
	add.Flags().StringVar(&discoveredFrom, "discovered-from", "", "run id that discovered this ticket")
	add.Flags().StringVar(&testStep, "test-step", "", "validation/test step that discovered this ticket")
	cmd.AddCommand(add)

	update := &cobra.Command{
		Use: "update <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			t, err := app.DB.UpdateTicket(context.Background(), args[0], title, body, "", typ)
			if err != nil {
				return err
			}
			return app.Printer.Success(t)
		},
	}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	update.Flags().StringVar(&typ, "type", "", "type")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use: "delete <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.requireYes("delete ticket"); err != nil {
				return err
			}
			if app.Flags.DryRun {
				return app.dryRun("ticket.delete", map[string]any{"id": args[0]})
			}
			if err := app.DB.DeleteTicket(context.Background(), args[0]); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"deleted": args[0]})
		},
	})

	claim := &cobra.Command{
		Use: "claim [ticket_id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if agent == "" {
				return app.Printer.Fail("missing_agent", "--agent required", "")
			}
			d, err := time.ParseDuration(lease)
			if err != nil {
				d = 45 * time.Minute
			}
			var claim any
			var ticket any
			if app.Flags.DryRun {
				return app.dryRun("ticket.claim", map[string]any{"ticket": args, "next": next, "epic": epicID, "agent": agent, "lease": lease})
			}
			if next || len(args) == 0 {
				if epicID == "" {
					return app.Printer.Fail("missing_epic", "--epic required with --next", "")
				}
				c, t, err := app.DB.ClaimNext(context.Background(), epicID, agent, d)
				if err != nil {
					return app.Printer.Fail("claim_failed", err.Error(), "")
				}
				claim, ticket = c, t
			} else {
				c, t, err := app.DB.ClaimTicket(context.Background(), args[0], agent, d)
				if err != nil {
					return app.Printer.Fail("claim_failed", err.Error(), "")
				}
				claim, ticket = c, t
			}
			return app.Printer.Success(map[string]any{"claim": claim, "ticket": ticket})
		},
	}
	claim.Flags().StringVar(&agent, "agent", "", "agent id")
	claim.Flags().StringVar(&lease, "lease", "45m", "lease duration")
	claim.Flags().StringVar(&epicID, "epic", "", "epic id")
	claim.Flags().BoolVar(&next, "next", false, "claim next ready ticket")
	cmd.AddCommand(claim)

	hb := &cobra.Command{
		Use: "heartbeat <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			d, _ := time.ParseDuration(lease)
			c, err := app.DB.HeartbeatClaim(context.Background(), args[0], agent, d)
			if err != nil {
				return err
			}
			return app.Printer.Success(c)
		},
	}
	hb.Flags().StringVar(&agent, "agent", "", "agent id")
	hb.Flags().StringVar(&lease, "lease", "45m", "lease")
	cmd.AddCommand(hb)

	rel := &cobra.Command{
		Use: "release <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.DB.ReleaseClaim(context.Background(), args[0], agent, reason); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"released": args[0], "reason": reason})
		},
	}
	rel.Flags().StringVar(&agent, "agent", "", "agent id")
	rel.Flags().StringVar(&reason, "reason", "", "reason")
	cmd.AddCommand(rel)

	closeCmd := &cobra.Command{
		Use: "close <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("ticket.close", map[string]any{"ticket": args[0], "agent": agent, "evidence": evidence})
			}
			t, err := app.DB.CloseTicket(context.Background(), args[0], agent, evidence)
			if err != nil {
				return app.Printer.Fail("close_failed", err.Error(), "")
			}
			return app.Printer.Success(t)
		},
	}
	closeCmd.Flags().StringVar(&agent, "agent", "", "agent id")
	closeCmd.Flags().StringVar(&evidence, "evidence", "", "receipt id")
	cmd.AddCommand(closeCmd)

	block := &cobra.Command{
		Use: "block <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.DB.AddDependency(context.Background(), args[0], by); err != nil {
				return err
			}
			_, _ = app.DB.UpdateTicket(context.Background(), args[0], "", "", "blocked", "")
			return app.Printer.Success(map[string]string{"blocked": args[0], "by": by})
		},
	}
	block.Flags().StringVar(&by, "by", "", "blocking ticket id")
	cmd.AddCommand(block)

	unblock := &cobra.Command{
		Use: "unblock <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.DB.RemoveDependency(context.Background(), args[0], by); err != nil {
				return err
			}
			_, _ = app.DB.UpdateTicket(context.Background(), args[0], "", "", "ready", "")
			return app.Printer.Success(map[string]string{"unblocked": args[0], "by": by})
		},
	}
	unblock.Flags().StringVar(&by, "by", "", "blocking ticket id")
	cmd.AddCommand(unblock)

	return cmd
}

func newWaveCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "wave", Short: "Inspect ticket waves"}
	var epicID string
	list := &cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if epicID == "" {
				return app.Printer.Fail("missing_epic", "--epic required", "")
			}
			waves, err := app.DB.ListWaves(context.Background(), epicID)
			if err != nil {
				return err
			}
			if len(waves) == 0 {
				waves, err = app.DB.ComputeWaves(context.Background(), epicID)
				if err != nil {
					return err
				}
			}
			return app.Printer.Success(waves)
		},
	}
	list.Flags().StringVar(&epicID, "epic", "", "epic id")
	cmd.AddCommand(list)
	cmd.AddCommand(&cobra.Command{
		Use: "view <wave_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			w, err := app.DB.GetWave(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(w)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status <wave_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			w, err := app.DB.GetWave(context.Background(), args[0])
			if err != nil {
				return err
			}
			tickets, _ := app.DB.ListTickets(context.Background(), w.EpicID)
			var inWave int
			for _, t := range tickets {
				if t.WaveID == w.ID {
					inWave++
				}
			}
			return app.Printer.Success(map[string]any{"wave": w, "tickets": inWave})
		},
	})
	return cmd
}
