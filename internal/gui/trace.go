package gui

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// trace is the record --ui-trace writes for a test of the built window: every event sent to the
// page, and what the page shows whenever that changes. One JSON object per line, with the
// milliseconds since the start: {"ms":…,"sent":<UIEvent>} or {"ms":…,"page":<what snapshotScript
// reports>}. It holds what the window shows, so the file is private and only a test asks for it.
type trace struct {
	mu    sync.Mutex
	file  *os.File
	start time.Time
	last  string
}

func openTrace(path string) (*trace, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return &trace{file: f, start: time.Now()}, nil
}

func (t *trace) write(kind string, value any) {
	line, err := json.Marshal(map[string]any{"ms": time.Since(t.start).Milliseconds(), kind: value})
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.file.Write(append(line, '\n'))
}

// sent records an event on its way to the page.
func (t *trace) sent(ev UIEvent) { t.write("sent", ev) }

// page records what the page reports of itself, when it differs from the last report.
func (t *trace) page(report string) {
	t.mu.Lock()
	same := report == t.last
	t.last = report
	t.mu.Unlock()
	if !same && json.Valid([]byte(report)) {
		t.write("page", json.RawMessage(report))
	}
}

// snapshotScript leaves in the page what it shows: its text, the skin, the bundled font families it
// has loaded to draw it, how many images it holds (the
// page has none of its own: one would be markup that got in), its size and scale and whether
// anything runs off its side, and where its buttons and fields are (so a test presses the button a
// person would, wherever a skin puts it). The page hands it over
// when it gets the probe event.
const snapshotScript = `document.documentElement.dataset.probe = JSON.stringify({
  text: document.body.innerText,
  skin: document.documentElement.dataset.skin || "",
  theme: document.documentElement.dataset.theme || "",
  fonts: [...new Set([...document.fonts].filter((f) => f.status === "loaded").map((f) => f.family.replace(/"/g, "")))].sort(),
  images: document.images.length,
  height: innerHeight,
  width: innerWidth,
  scale: devicePixelRatio,
  overflows: document.documentElement.scrollWidth > innerWidth,
  clipped: [...document.querySelectorAll("button, input, select, [role=status]")].filter((e) => {
    const r = e.getBoundingClientRect();
    return r.width > 0 && (r.left < -1 || r.right > innerWidth + 1);
  }).length,
  buttons: [...document.querySelectorAll("button")].map((b) => {
    const r = b.getBoundingClientRect();
    return {
      label: (b.innerText || b.getAttribute("aria-label") || "").trim(),
      x: Math.round(r.left + r.width / 2),
      y: Math.round(r.top + r.height / 2),
      disabled: b.disabled,
    };
  }),
  fields: [...document.querySelectorAll("input:not([type=checkbox]), select, textarea")].map((f) => {
    const r = f.getBoundingClientRect();
    return {
      label: (f.getAttribute("aria-label") || f.closest("label")?.innerText || f.placeholder || "").trim(),
      x: Math.round(r.left + r.width / 2),
      y: Math.round(r.top + r.height / 2),
    };
  }),
})`

// snapshotEvery is how often the page is asked what it shows.
const snapshotEvery = 250 * time.Millisecond
