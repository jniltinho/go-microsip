package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/control"
	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// service is the softphone for a machine without a desktop session: it owns the profile as the
// window does (the instance lock, the database, SIP and the sound) and serves agent control with
// the single-use bootstrap grant a person exported from the window, and nothing else. It never
// asks anybody: what the grant does not cover is answered grant_required. A window or a link
// opened meanwhile is turned away.
//
// Exit statuses: 0 stopped by a signal, 2 invalid arguments or grant file, 3 the bootstrap grant
// cannot be used, 4 something it needs is unavailable, 1 otherwise, as for every command when the
// configuration cannot be used.
func (s *session) service() *cobra.Command {
	var grantFile string
	c := &cobra.Command{
		Use:   "service --grant-file FILE",
		Short: "Run the softphone without a window, serving only what a bootstrap grant allows",
		Long: `Registers the configured account and serves the control commands and the MCP server with the
bootstrap grant in FILE, exported from the window (Settings, Agent access, Export for headless
service). A bootstrap starts one service session; a new session needs a new export. The service
needs a password from account.password_env or a private account.password_file: it never prompts
and never reads the keyring. It says on the standard error when the microphone becomes active,
and stops on SIGTERM or an interrupt, ending calls and unregistering first.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			unavailable := func(message string) error { return &failure{message: "go-microsip: " + message, code: 4} }
			if grantFile == "" {
				return usage("usage: go-microsip service --grant-file FILE [--config FILE] [--env-file FILE]")
			}
			bootstrapID, capability, err := control.ReadBootstrap(grantFile, s.platform.Files)
			if err != nil {
				return usage(err.Error())
			}
			dir, err := control.RuntimeDir(s.getenv)
			if err != nil {
				return unavailable("runtime_unavailable: " + err.Error())
			}
			log := func(message string) { fmt.Fprintln(s.stderr, "go-microsip:", message) }
			options := s.options()
			options.Secrets, options.LegacySecrets = nil, nil // an explicit password source only
			if err := app.Validate(options); err != nil {
				return invalidConfiguration(err)
			}
			phone, err := app.OpenPhone(options, app.PhoneOptions{Audio: s.platform.Audio, UserAgent: "go-microsip/" + s.version,
				Log: slog.New(slog.NewTextHandler(s.stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))})
			switch {
			case errors.Is(err, app.ErrRunning):
				return unavailable("another go-microsip instance owns the profile; quit it first")
			case err != nil:
				return failed(err)
			}
			defer phone.Close() // calls end and the account unregisters, within the engine's bound
			if settings := phone.Settings(); settings.Account == nil || !settings.HasPassword {
				return unavailable("secrets_unavailable: set account.password_env or a private account.password_file (the service never prompts)")
			}
			owner, err := control.NewServiceOwner(phone, bootstrapID, capability)
			switch {
			case errors.Is(err, control.ErrBootstrapConsumed), errors.Is(err, control.ErrBootstrapInvalid):
				return &failure{message: "go-microsip: " + err.Error(), code: 3}
			case errors.Is(err, control.ErrNoLedger):
				return unavailable("storage_unavailable: " + err.Error())
			case err != nil:
				return failed(err)
			}
			defer owner.Close()
			endpoint, err := control.Listen(dir, owner.Welcome(), owner)
			if err != nil {
				return unavailable("control socket unavailable: " + err.Error())
			}
			defer endpoint.Close()
			signals := make(chan os.Signal, 2)
			signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(signals)
			log(fmt.Sprintf("service ready (epoch %d)", owner.Epoch()))
			events, active := phone.Events(), false
			for {
				select {
				case <-signals:
					log("service stopping")
					return nil
				case ev, open := <-events:
					if !open {
						return failed(errors.New("the telephony engine stopped"))
					}
					owner.Observe(ev)
					// The sound devices are open while a call's media runs, and only then.
					if call, is := ev.(domain.CallChanged); is {
						switch now := call.Call.Phase.Kind == domain.Established || call.Call.Phase.Kind == domain.Held; {
						case now && !active:
							log("microphone active (a call's media is running)")
						case !now && active && call.Call.Phase.IsFinal():
							log("microphone released")
						}
						if call.Call.Phase.IsFinal() {
							active = false
						} else if call.Call.Phase.Kind == domain.Established || call.Call.Phase.Kind == domain.Held {
							active = true
						}
					}
				}
			}
		},
	}
	c.Flags().StringVar(&grantFile, "grant-file", "", "the bootstrap grant `file` exported from the window")
	return c
}

// servedByService reports whether the headless service owns the profile of this session.
func (s *session) servedByService() bool {
	dir, err := control.RuntimeDir(s.getenv)
	if err != nil {
		return false
	}
	client, err := control.Dial(dir)
	if err != nil {
		return false
	}
	defer client.Close()
	return client.Welcome().Mode == control.ModeService
}
