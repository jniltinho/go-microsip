package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// run is the softphone without a window. It exits 0 when it was asked to stop or the call it
// placed has ended, 1 when the configuration, the registration or that call failed, and 130 or 143
// after an interrupt or a termination signal, once calls are ended and the account is
// unregistered.
func (s *session) run() *cobra.Command {
	var (
		call   string
		hangup time.Duration
		asJSON bool
	)
	c := &cobra.Command{
		Use:   "run",
		Short: "Run the softphone without a window: register, take calls, optionally place one",
		Long: `Registers the configured account and reports registration and call events, one per line, until
it is interrupted. With --call it dials the number once registered and exits when that call has
ended. It needs a password from account.password_env or a private account.password_file, and it
opens no window.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()
			var signaled atomic.Int32
			signals := make(chan os.Signal, 2)
			signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(signals)
			go func() {
				select {
				case sig := <-signals:
					code := int32(130)
					if sig == syscall.SIGTERM {
						code = 143
					}
					signaled.Store(code)
					cancel()
				case <-ctx.Done():
				}
			}()
			err := app.Run(ctx, s.options(), app.RunOptions{
				Audio: s.platform.Audio, Call: call, HangupAfter: hangup, UserAgent: "go-microsip/" + s.version,
				Log: slog.New(slog.NewTextHandler(s.stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
			}, printer(c.OutOrStdout(), asJSON))
			switch code := int(signaled.Load()); {
			case err != nil && app.IsConfiguration(err):
				return invalidConfiguration(err)
			case err != nil:
				return failed(err)
			case code != 0:
				return &failure{code: code}
			}
			return nil
		},
	}
	c.Flags().StringVar(&call, "call", "", "dial this `number` once registered and exit when the call has ended")
	c.Flags().DurationVar(&hangup, "hangup-after", 0, "with --call: hang up this long after the answer (for example 10s)")
	c.Flags().BoolVar(&asJSON, "json", false, "print each event as one JSON object")
	return c
}

// printer writes one line per engine event. It never prints the other party's number.
func printer(out io.Writer, asJSON bool) func(domain.Event) {
	type line struct {
		Event     string `json:"event"`
		State     string `json:"state,omitempty"`
		Call      uint64 `json:"call,omitempty"`
		Direction string `json:"direction,omitempty"`
		Secure    *bool  `json:"secure,omitempty"`
		Muted     *bool  `json:"muted,omitempty"`
		Transfer  string `json:"transfer,omitempty"`
	}
	return func(ev domain.Event) {
		var l line
		text := ""
		switch ev := ev.(type) {
		case domain.RegistrationChanged:
			l = line{Event: "registration", State: ev.State.String()}
			text = "registration: " + l.State
		case domain.CallChanged:
			c := ev.Call
			l = line{Event: "call", State: c.Phase.String(), Call: uint64(c.ID), Direction: c.Direction.String(), Secure: &c.Secure, Muted: &c.Muted}
			text = fmt.Sprintf("call %d: %s %s", c.ID, l.Direction, l.State)
			if c.Secure {
				text += ", secure"
			}
			if c.Muted {
				text += ", muted"
			}
			if c.Transfer.Kind != domain.NoTransfer {
				l.Transfer = c.Transfer.Kind.String()
				text += ", transfer " + l.Transfer
			}
		case domain.Noticed:
			l = line{Event: "notice", State: ev.Notice.String()}
			text = "notice: " + l.State
		case domain.Stopped:
			l, text = line{Event: "stopped"}, "stopped"
		default:
			return
		}
		if asJSON {
			_ = json.NewEncoder(out).Encode(l)
			return
		}
		fmt.Fprintln(out, text)
	}
}
