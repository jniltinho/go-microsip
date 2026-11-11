package g0

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	dmedia "github.com/emiago/diago/media"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// AppScenarios run the softphone without a window (app.Run, what `go-microsip run` calls) against
// the lab, from a configuration file to the call history (openspec/changes/migrate-to-go-wails,
// task 3.9). The sound is the in-memory one; real devices are task 3.8.
func AppScenarios() []Scenario {
	return []Scenario{
		{"app/run-call/1001", appRunCall("1001")},
		{"app/run-call/1201", appRunCall("1201")},
	}
}

// labFiles is the file access of the runner: the lab's fixtures are its own.
type labFiles struct{}

func (labFiles) CurrentUser() uint32 { return 1 }
func (labFiles) Inspect(info os.FileInfo) (owner, mode uint32) {
	return 1, uint32(info.Mode().Perm())
}

// appRunCall writes the account as a TOML file, takes the password from the environment, and lets
// the softphone call the echo extension for three seconds.
func appRunCall(user string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		a := l.Account(user, dmedia.CodecAudioAlaw)
		dir, err := os.MkdirTemp("", "g0-app-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		text := fmt.Sprintf("schema = 1\n[account]\nusername = %q\ndomain = %q\nport = %d\ntransport = %q\nexpires = 120\n", a.User, a.Server, a.Port, a.Transport)
		if a.SecureMedia {
			text += "media = \"srtp\"\n"
		}
		if a.CAFile != "" {
			text += fmt.Sprintf("ca_file = %q\n", a.CAFile)
		}
		file := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			return nil, err
		}
		o := app.Options{
			ConfigPath: file, Files: labFiles{},
			Env:  map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "GO_MICROSIP_SIP_PASSWORD": a.Password},
			Warn: func(m string) { l.Log.Warn("app", "warning", m) },
		}
		sound := &memory.Audio{Tone: toneA}
		var mu sync.Mutex
		var events []domain.Event
		started := time.Now()
		err = app.Run(ctx, o, app.RunOptions{Audio: sound, Call: "600", HangupAfter: 3 * time.Second, Log: l.Log}, func(ev domain.Event) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		})
		data := map[string]any{"events": len(events), "took_ms": time.Since(started).Milliseconds()}
		if err != nil {
			return data, fmt.Errorf("run: %w", err)
		}
		var established, final domain.CallSnapshot
		for _, ev := range events {
			if c, ok := ev.(domain.CallChanged); ok {
				if c.Call.Phase.Kind == domain.Established {
					established = c.Call
				}
				if c.Call.Phase.IsFinal() {
					final = c.Call
				}
			}
		}
		if _, stopped := events[len(events)-1].(domain.Stopped); !stopped {
			return data, errors.New("Stopped is not the last event")
		}
		if established.ID == 0 || established.Secure != a.SecureMedia || final.Phase != domain.EndedWith(domain.EndNormal) {
			return data, fmt.Errorf("the call: established %v, ended %v", established, final.Phase)
		}
		stream := sound.Last()
		if stream == nil {
			return data, errors.New("the sound devices were never opened")
		}
		frames, share := stream.Heard(toneA)
		data["heard"], data["frames"] = share[toneA], frames
		if share[toneA] <= 0.5 || sound.OpenStreams() != 0 {
			return data, fmt.Errorf("heard %.2f of the echo; %d streams left open", share[toneA], sound.OpenStreams())
		}

		// The call is in the history as the user would see it.
		s, err := store.Open(app.StorePath(o.Env))
		if err != nil {
			return data, err
		}
		defer s.Close()
		calls, err := s.RecentCalls(10, store.AllCalls, "")
		if err != nil || len(calls) != 1 {
			return data, fmt.Errorf("history: %d calls, %v", len(calls), err)
		}
		c := calls[0].Call
		if c.Outcome != store.Answered || c.Remote != "600" || c.Secure != a.SecureMedia || c.DurationMS == nil || *c.DurationMS < 2500 || c.EndedUTC == nil {
			return data, fmt.Errorf("journaled call %+v", c)
		}
		data["duration_ms"] = *c.DurationMS
		return data, nil
	}
}
