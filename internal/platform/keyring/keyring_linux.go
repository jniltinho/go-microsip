package keyring

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// The Secret Service (org.freedesktop.secrets), as the Rust candidate uses it: items of the
// default collection, found by the attributes application and account. The session is "plain":
// the secret crosses the user's own session bus, which only the same user can reach and who can
// read the keyring anyway.
const (
	service        = "org.freedesktop.secrets"
	servicePath    = "/org/freedesktop/secrets"
	defaultAlias   = "/org/freedesktop/secrets/aliases/default"
	serviceIface   = "org.freedesktop.Secret.Service"
	collectionItem = "org.freedesktop.Secret.Collection.CreateItem"
	itemIface      = "org.freedesktop.Secret.Item"
	// callTimeout bounds one whole operation on the keyring (a handful of calls): the window
	// waits for it when it starts.
	callTimeout = 5 * time.Second
)

// secret is the Secret Service's (oayays) structure.
type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// link is one operation's connection to the keyring.
type link struct {
	conn *dbus.Conn
	ctx  context.Context
}

// open connects to the session bus, for an operation whose time is ctx's. The connection is the
// process's shared one and is not closed. Every call of the operation is made with ctx, and the
// bus library starts no call for a context that is over. A call already sent is the keyring's to
// finish, whenever it does: callers give every password an entry of its own for that.
func open(ctx context.Context) (*link, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, ports.ErrNoSecretService
	}
	if ctx.Err() != nil {
		return nil, ErrTimeout // connecting took the operation's whole time
	}
	return &link{conn: conn, ctx: ctx}, nil
}

func (l *link) call(path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return l.conn.Object(service, path).CallWithContext(l.ctx, method, 0, args...)
}

// failure maps what the bus or the keyring answered. Only the error's name is kept.
func failure(err error) error {
	var named dbus.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return ErrTimeout
	case errors.As(err, &named):
		switch named.Name {
		case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner",
			"org.freedesktop.DBus.Error.Disconnected", "org.freedesktop.DBus.Error.Spawn.ChildExited":
			return ports.ErrNoSecretService
		case "org.freedesktop.DBus.Error.NoReply", "org.freedesktop.DBus.Error.TimedOut", "org.freedesktop.DBus.Error.Timeout":
			return ErrTimeout
		case "org.freedesktop.Secret.Error.IsLocked":
			return ErrLocked
		case "org.freedesktop.Secret.Error.NoSuchObject", "org.freedesktop.DBus.Error.UnknownObject", "org.freedesktop.DBus.Error.UnknownMethod":
			return errors.New("the keyring has no default collection")
		}
		return fmt.Errorf("keyring error: %s", named.Name)
	}
	return ports.ErrNoSecretService
}

func (l *link) session() (dbus.ObjectPath, error) {
	var output dbus.Variant
	var session dbus.ObjectPath
	err := l.call(servicePath, serviceIface+".OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &session)
	return session, failure(err)
}

func (l *link) close(session dbus.ObjectPath) {
	_ = l.call(session, "org.freedesktop.Secret.Session.Close").Err
}

// dismiss refuses a prompt the keyring wants to show: nothing is ever asked of the user from here.
func (l *link) dismiss(prompt dbus.ObjectPath) {
	_ = l.call(prompt, "org.freedesktop.Secret.Prompt.Dismiss").Err
}

func attributes(owner, ref string) map[string]string {
	return map[string]string{"application": owner, "account": ref}
}

func (l *link) search(owner, ref string) (unlocked, locked []dbus.ObjectPath, err error) {
	err = l.call(servicePath, serviceIface+".SearchItems", attributes(owner, ref)).Store(&unlocked, &locked)
	return unlocked, locked, failure(err)
}

// within runs one operation on the keyring within callTimeout, connecting to the session bus
// included: a bus that accepts a connection and then says nothing must not hold the window.
func within[T any](op func(context.Context) (T, error)) (T, error) {
	return withinFor(callTimeout, op)
}

// withinFor gives the operation a context that ends after d and waits for it no longer than
// that. An operation that is given up on finds its context over: whatever it had not yet sent, it
// can no longer send, so it cannot write after the caller was told the time was up. What it had
// already sent may still be taken by the keyring: for a write, that outcome is not known.
func withinFor[T any](d time.Duration, op func(context.Context) (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	done := make(chan result, 1)
	go func() {
		defer cancel()
		value, err := op(ctx)
		done <- result{value, err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		select {
		case r := <-done: // it finished as its time ran out
			return r.value, r.err
		default:
		}
		var none T
		return none, ErrTimeout
	}
}

// found is what a read gives.
type found struct {
	password string
	ok       bool
}

// Get returns the password stored under ref; ok is false when there is none.
func (Store) Get(ref string) (password string, ok bool, err error) {
	f, err := within(func(ctx context.Context) (found, error) { return get(ctx, application, ref) })
	return f.password, f.ok, err
}

// Get reads the password the Rust application stored under ref. Its entries are only ever read.
func (Legacy) Get(ref string) (password string, ok bool, err error) {
	f, err := within(func(ctx context.Context) (found, error) { return get(ctx, legacyApplication, ref) })
	return f.password, f.ok, err
}

func get(ctx context.Context, owner, ref string) (found, error) {
	password, ok, err := read(ctx, owner, ref)
	return found{password, ok}, err
}

func read(ctx context.Context, owner, ref string) (password string, ok bool, err error) {
	if err := validRef(ref); err != nil {
		return "", false, err
	}
	l, err := open(ctx)
	if err != nil {
		return "", false, err
	}
	unlocked, locked, err := l.search(owner, ref)
	switch {
	case err != nil:
		return "", false, err
	case len(unlocked) == 0 && len(locked) > 0:
		return "", false, ErrLocked
	case len(unlocked) == 0:
		return "", false, nil
	}
	session, err := l.session()
	if err != nil {
		return "", false, err
	}
	defer l.close(session)
	var found secret
	if err := l.call(unlocked[0], itemIface+".GetSecret", session).Store(&found); err != nil {
		return "", false, failure(err)
	}
	return string(found.Value), true, nil
}

// Set stores or replaces the password under ref in the default collection.
func (Store) Set(ref, password string) error {
	_, err := within(func(ctx context.Context) (struct{}, error) { return struct{}{}, store(ctx, ref, password) })
	return err
}

func store(ctx context.Context, ref, password string) error {
	if err := validRef(ref); err != nil {
		return err
	}
	l, err := open(ctx)
	if err != nil {
		return err
	}
	session, err := l.session()
	if err != nil {
		return err
	}
	defer l.close(session)
	properties := map[string]dbus.Variant{
		itemIface + ".Label":      dbus.MakeVariant("go-microsip SIP account"), // shown by keyring managers: never the secret
		itemIface + ".Attributes": dbus.MakeVariant(attributes(application, ref)),
	}
	value := secret{Session: session, Parameters: []byte{}, Value: []byte(password), ContentType: "text/plain; charset=utf8"}
	var item, prompt dbus.ObjectPath
	if err := l.call(defaultAlias, collectionItem, properties, value, true).Store(&item, &prompt); err != nil {
		return failure(err)
	}
	if prompt != "/" {
		l.dismiss(prompt)
		return ErrLocked
	}
	return nil
}

// Delete removes every entry stored under ref; none is not an error.
func (Store) Delete(ref string) error {
	_, err := within(func(ctx context.Context) (struct{}, error) { return struct{}{}, remove(ctx, ref) })
	return err
}

func remove(ctx context.Context, ref string) error {
	if err := validRef(ref); err != nil {
		return err
	}
	l, err := open(ctx)
	if err != nil {
		return err
	}
	unlocked, locked, err := l.search(application, ref)
	if err != nil {
		return err
	}
	for _, item := range unlocked {
		var prompt dbus.ObjectPath
		if err := l.call(item, itemIface+".Delete").Store(&prompt); err != nil {
			return failure(err)
		}
		if prompt != "/" {
			l.dismiss(prompt)
			return ErrLocked
		}
	}
	if len(locked) > 0 {
		return ErrLocked
	}
	return nil
}
