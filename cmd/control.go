package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jniltinho/go-microsip/internal/control"
)

// The control commands talk to the go-microsip that is running, over the control socket of the
// session. They never open the database, register SIP or start a softphone: without one they
// report engine_unavailable.
//
// A mutation carries an idempotency key: --key to retry safely, otherwise a random one, printed
// so that the operator can ask about the operation or try again with it. DTMF digits and the
// target of a call or a transfer can come from the standard input ("-"), which keeps them out
// of the process list. Keys stay in the local ledger for 24 hours: never put personal data in one.
//
// Exit statuses: 0 done or taken, 2 invalid, 3 not authorized or waiting for approval,
// 4 not running or unavailable, 5 timeout, 1 internal.

// controlFlags are the options every control command takes.
type controlFlags struct {
	json      bool
	key       string
	grantFile string
}

func (f *controlFlags) add(c *cobra.Command, mutation bool) {
	c.Flags().BoolVar(&f.json, "json", false, "print the answer as one JSON object")
	c.Flags().StringVar(&f.grantFile, "grant-file", "", "use the grant in this private `file`, created in Settings, Agent access")
	if mutation {
		c.Flags().StringVar(&f.key, "key", "", "idempotency `key`: the same key again never repeats the effect")
	}
}

// usage is an argument the command cannot use: status 2, and the argument is not repeated.
func usage(message string) error { return &failure{message: "go-microsip: " + message, code: 2} }

// random returns "<prefix>-<24 hexadecimal digits>".
func random(prefix string) string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%s-%d", prefix, os.Getpid())
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}

// reach sends one request to the owner of this session and returns its answer.
func (s *session) reach(r control.Request) control.Response {
	dir, err := control.RuntimeDir(map[string]string{"XDG_RUNTIME_DIR": s.getenv("XDG_RUNTIME_DIR")})
	if err != nil {
		return control.Failure(r.RequestID, &control.Error{Code: control.EngineUnavailable, Message: "no runtime directory of this session; nothing was started"})
	}
	client, err := control.Dial(dir)
	switch {
	case errors.Is(err, control.ErrUnavailable):
		return control.Failure(r.RequestID, &control.Error{Code: control.EngineUnavailable, Message: "go-microsip is not running; nothing was started"})
	case errors.Is(err, control.ErrBusy):
		return control.Failure(r.RequestID, &control.Error{Code: control.Busy, Message: "the running instance is busy; try again"})
	case err != nil:
		return control.Failure(r.RequestID, &control.Error{Code: control.EngineUnavailable, Message: "the running instance cannot be reached: " + err.Error()})
	}
	defer client.Close()
	return client.Do(r)
}

// capability reads the grant of --grant-file; empty without one.
func (s *session) capability(path string) (*string, error) {
	if path == "" {
		return nil, nil
	}
	capability, err := control.ReadCapability(path, s.platform.Files)
	if err != nil {
		return nil, usage(err.Error())
	}
	return &capability, nil
}

// ask runs one control command: the request, the owner's answer, its rendering and its status.
func (s *session) ask(c *cobra.Command, f *controlFlags, op control.Operation) error {
	capability, err := s.capability(f.grantFile)
	if err != nil {
		return err
	}
	r := control.Request{SchemaVersion: control.SchemaVersion, RequestID: random("req"), Operation: op, Capability: capability}
	if op.Mutation() {
		key := f.key
		if key == "" {
			key = random("cli")
		}
		r.IdempotencyKey = &key
	}
	response := control.Failure(r.RequestID, r.Validate())
	if r.Validate() == nil {
		response = s.reach(r)
	}
	out := c.OutOrStdout()
	if f.json {
		_ = json.NewEncoder(out).Encode(response)
	} else {
		fmt.Fprint(out, render(response, r.IdempotencyKey))
	}
	if code := response.ExitCode(); code != 0 {
		return &failure{code: code}
	}
	return nil
}

// render is an answer for a person.
func render(r control.Response, key *string) string {
	var b strings.Builder
	switch r.Status {
	case control.StatusOK:
		b.WriteString("ok\n")
	case control.StatusAccepted:
		b.WriteString("accepted\n")
	case control.StatusApprovalRequired:
		b.WriteString("waiting for approval in the go-microsip window\n")
	case control.StatusError:
		if r.Error != nil {
			fmt.Fprintf(&b, "error: %s (%s)\n", r.Error.Message, r.Error.Code)
		}
	}
	if r.OperationID != nil {
		fmt.Fprintf(&b, "operation: %s\n", *r.OperationID)
	}
	if key != nil {
		fmt.Fprintf(&b, "idempotency key: %s\n", *key)
	}
	if len(r.Data) > 0 && string(r.Data) != "null" {
		var pretty any
		if json.Unmarshal(r.Data, &pretty) == nil {
			text, _ := json.MarshalIndent(pretty, "", "  ")
			b.Write(text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// orStdin is an argument, or one line of the standard input when the argument is "-".
func orStdin(c *cobra.Command, argument string) string {
	if argument != "-" {
		return argument
	}
	line, _ := bufio.NewReader(io.LimitReader(c.InOrStdin(), 128)).ReadString('\n')
	return strings.TrimSpace(line)
}

// callRef reads CALL_ID and VERSION, as status prints them.
func callRef(args []string) (id, version uint64, err error) {
	if id, err = strconv.ParseUint(args[0], 10, 64); err != nil {
		return 0, 0, usage("CALL_ID must be a number")
	}
	if version, err = strconv.ParseUint(args[1], 10, 64); err != nil {
		return 0, 0, usage("VERSION must be a number")
	}
	return id, version, nil
}

// group is a command that only holds others.
func group(use, short string, commands ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	c.AddCommand(commands...)
	return c
}

// controlCommands are the commands that talk to the running softphone, but for `config show`,
// which is under `config`.
func (s *session) controlCommands() []*cobra.Command {
	read := func(use, short string, args cobra.PositionalArgs, op func(args []string, limit uint32, cursor, filter string) control.Operation, paged, filtered bool) *cobra.Command {
		var f controlFlags
		var limit uint32
		var cursor, filter string
		c := &cobra.Command{Use: use, Short: short, Args: args, RunE: func(c *cobra.Command, args []string) error {
			o := op(args, limit, cursor, filter)
			if paged && c.Flags().Changed("limit") { // a limit of 0 is sent, and refused, as it was typed
				o.Limit = &limit
			}
			return s.ask(c, &f, o)
		}}
		f.add(c, false)
		if paged {
			c.Flags().Uint32Var(&limit, "limit", 0, "page size, 1 to 100")
			c.Flags().StringVar(&cursor, "cursor", "", "the next_cursor of the page before")
		}
		if filtered {
			c.Flags().StringVar(&filter, "filter", "", "all, incoming, outgoing or missed")
		}
		return c
	}
	page := func(o control.Operation, limit uint32, cursor string) control.Operation {
		if limit != 0 {
			o.Limit = &limit
		}
		if cursor != "" {
			o.Cursor = &cursor
		}
		return o
	}
	// on is a mutation of the current call: CALL_ID and VERSION first, then what the command adds.
	on := func(use, short string, extra int, op func(c *cobra.Command, o control.Operation, rest []string) control.Operation) *cobra.Command {
		var f controlFlags
		c := &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(2 + extra), RunE: func(c *cobra.Command, args []string) error {
			id, version, err := callRef(args)
			if err != nil {
				return err
			}
			return s.ask(c, &f, op(c, control.Operation{CallID: id, ExpectedVersion: version}, args[2:]))
		}}
		f.add(c, true)
		return c
	}
	named := func(name string) func(*cobra.Command, control.Operation, []string) control.Operation {
		return func(_ *cobra.Command, o control.Operation, _ []string) control.Operation { o.Op = name; return o }
	}
	flag := func(name string, set func(*control.Operation)) func(*cobra.Command, control.Operation, []string) control.Operation {
		return func(_ *cobra.Command, o control.Operation, _ []string) control.Operation {
			o.Op = name
			set(&o)
			return o
		}
	}
	var dial, dnd controlFlags
	dialCommand := &cobra.Command{Use: "dial TARGET|-", Short: "Place a call; - reads the number from the standard input", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return s.ask(c, &dial, control.Operation{Op: control.CallDial, Target: orStdin(c, args[0])})
		}}
	dial.add(dialCommand, true)
	dndCommand := &cobra.Command{Use: "dnd on|off", Short: "Turn do-not-disturb on or off", Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if args[0] != "on" && args[0] != "off" {
				return usage("dnd takes on or off")
			}
			return s.ask(c, &dnd, control.Operation{Op: control.DNDSet, Enabled: args[0] == "on"})
		}}
	dnd.add(dndCommand, true)
	return []*cobra.Command{
		read("status", "Registration, do-not-disturb and the current call of the running softphone (no names or numbers)", cobra.NoArgs,
			func([]string, uint32, string, string) control.Operation {
				return control.Operation{Op: control.StatusGet}
			}, false, false),
		group("operation", "Ask about an earlier request",
			read("get OPERATION_ID", "State and outcome of an earlier request", cobra.ExactArgs(1),
				func(args []string, _ uint32, _, _ string) control.Operation {
					return control.Operation{Op: control.OperationGet, OperationID: args[0]}
				}, false, false)),
		group("devices", "Audio devices of the running softphone",
			read("list", "List the audio devices (needs a read grant)", cobra.NoArgs,
				func([]string, uint32, string, string) control.Operation {
					return control.Operation{Op: control.DevicesList}
				}, false, false)),
		group("contacts", "Contacts of the running softphone",
			read("search [TEXT]", "Search the contacts (needs a read grant)", cobra.MaximumNArgs(1),
				func(args []string, limit uint32, cursor, _ string) control.Operation {
					o := control.Operation{Op: control.ContactsSearch}
					if len(args) == 1 {
						o.Query = args[0]
					}
					return page(o, limit, cursor)
				}, true, false)),
		group("history", "Call history of the running softphone",
			read("list", "List the calls (needs a read grant)", cobra.NoArgs,
				func(_ []string, limit uint32, cursor, filter string) control.Operation {
					o := control.Operation{Op: control.HistoryList}
					if filter != "" {
						o.Filter = &filter
					}
					return page(o, limit, cursor)
				}, true, true)),
		group("call", "Place and control calls through the running softphone",
			dialCommand,
			on("answer CALL_ID VERSION", "Answer the ringing call", 0, named(control.CallAnswer)),
			on("reject CALL_ID VERSION", "Reject the ringing call", 0, named(control.CallReject)),
			on("hangup CALL_ID VERSION", "End the call", 0, named(control.CallHangup)),
			on("mute CALL_ID VERSION", "Mute the microphone", 0, flag(control.CallMute, func(o *control.Operation) { o.Muted = true })),
			on("unmute CALL_ID VERSION", "Unmute the microphone", 0, named(control.CallMute)),
			on("hold CALL_ID VERSION", "Put the call on hold", 0, flag(control.CallHold, func(o *control.Operation) { o.Held = true })),
			on("resume CALL_ID VERSION", "Resume the call", 0, named(control.CallHold)),
			on("dtmf CALL_ID VERSION DIGITS|-", "Send DTMF digits; - reads them from the standard input", 1,
				func(c *cobra.Command, o control.Operation, rest []string) control.Operation {
					o.Op, o.Digits = control.CallDTMF, orStdin(c, rest[0])
					return o
				}),
			on("transfer CALL_ID VERSION TARGET|-", "Blind-transfer the call; - reads the number from the standard input", 1,
				func(c *cobra.Command, o control.Operation, rest []string) control.Operation {
					o.Op, o.Target = control.CallTransfer, orStdin(c, rest[0])
					return o
				})),
		dndCommand,
	}
}

// configShow is `config show`: the running softphone's configuration, without secrets.
func (s *session) configShow() *cobra.Command {
	var f controlFlags
	c := &cobra.Command{Use: "show", Short: "Show the running softphone's configuration, never secrets (needs a read grant)", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return s.ask(c, &f, control.Operation{Op: control.ConfigShow})
		}}
	f.add(c, false)
	return c
}
