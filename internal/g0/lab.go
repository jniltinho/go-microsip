package g0

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
)

// Lab is the disposable Asterisk lab as the runner sees it: the registrar's name and the fixtures
// that docker/asterisk/lab.py generated (fictional accounts, the lab CA and an unrelated CA).
type Lab struct {
	Server    string
	Dir       string
	Passwords map[string]string
	Log       *slog.Logger
}

// LoadLab reads the fixture directory mounted into the runner.
func LoadLab(dir, server string, log *slog.Logger) (*Lab, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "credentials.json"))
	if err != nil {
		return nil, fmt.Errorf("read lab credentials: %w", err)
	}
	l := &Lab{Server: server, Dir: dir, Log: log}
	if err := json.Unmarshal(raw, &l.Passwords); err != nil {
		return nil, fmt.Errorf("parse lab credentials: %w", err)
	}
	return l, nil
}

// Account returns a lab account. The lab maps the number to its transport: 10xx UDP, 11xx TCP,
// 12xx TLS with SDES-SRTP, 13xx TLS with plain RTP.
func (l *Lab) Account(user string, codec media.Codec) Account {
	a := Account{User: user, Password: l.Passwords[user], Server: l.Server, Port: 5060, Transport: "udp", Codec: codec}
	switch user[:2] {
	case "11":
		a.Transport = "tcp"
	case "12", "13":
		a.Transport, a.Port, a.CAFile = "tls", 5061, filepath.Join(l.Dir, "ca.crt")
		a.SecureMedia = user[:2] == "12"
	}
	return a
}

// Up creates an endpoint, starts serving incoming calls and registers it.
func (l *Lab) Up(ctx context.Context, a Account) (*Endpoint, error) {
	e, err := NewEndpoint(a, l.Log)
	if err != nil {
		return nil, err
	}
	if err := e.Serve(ctx); err != nil {
		e.Close()
		return nil, fmt.Errorf("serve: %w", err)
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := e.Register(rctx); err != nil {
		e.Close()
		return nil, fmt.Errorf("register %s: %w", a, err)
	}
	return e, nil
}

// Result is one scenario's outcome in the report.
type Result struct {
	Name   string         `json:"name"`
	OK     bool           `json:"ok"`
	Detail string         `json:"detail,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

// Scenario is one named lab probe.
type Scenario struct {
	Name string
	Run  func(ctx context.Context, l *Lab) (map[string]any, error)
}

// Run executes the scenarios in order, each with its own deadline, and never stops at a failure.
func (l *Lab) Run(ctx context.Context, list []Scenario) []Result {
	var out []Result
	for _, s := range list {
		sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		data, err := s.Run(sctx, l)
		cancel()
		r := Result{Name: s.Name, OK: err == nil, Data: data}
		if err != nil {
			r.Detail = err.Error()
		}
		l.Log.Info("scenario", "name", s.Name, "ok", r.OK, "detail", r.Detail)
		out = append(out, r)
		time.Sleep(300 * time.Millisecond) // let the registrar drop the previous binding
	}
	return out
}

// hangup ends a call and releases its media, ignoring a peer that already hung up.
func hangup(d interface {
	Hangup(context.Context) error
	Close() error
}, med *diago.DialogMedia) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = d.Hangup(ctx)
	if med != nil {
		_ = med.Close()
	}
	_ = d.Close()
}
