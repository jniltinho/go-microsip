package cmd

// The command-line contracts of the Rust candidate (apps/rust-microsip/tests/cli.rs) with the Cobra
// syntax: fictional data, no network, no devices, no display.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const marker = "fictional-secret-marker-7731"

// me owns every file of these tests.
type me struct{}

func (me) CurrentUser() uint32 { return 1000 }
func (me) Inspect(info os.FileInfo) (owner, mode uint32) {
	return 1000, uint32(info.Mode().Perm())
}

// cli runs one command line with exactly this environment and returns its status and everything it
// printed.
func cli(env map[string]string, args ...string) (int, string) {
	var out bytes.Buffer
	if env == nil {
		env = map[string]string{}
	}
	code := execute(&session{platform: Platform{Files: me{}}, env: env, stdout: &out, stderr: &out, version: "1.2.3", commit: "abc1234"}, args)
	return code, out.String()
}

func write(t *testing.T, path, text string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func config(t *testing.T) string {
	return write(t, filepath.Join(t.TempDir(), "config.toml"),
		"schema = 1\n[account]\nusername = \"1001\"\ndomain = \"pbx.invalid\"\nserver = \"pbx.invalid\"\ntransport = \"udp\"\nmedia = \"rtp\"\n")
}

func TestHelpAndVersionNeedNoConfiguration(t *testing.T) {
	if code, out := cli(nil, "--help"); code != 0 || !strings.Contains(out, "go-microsip [command]") || !strings.Contains(out, "never accepted as arguments") {
		t.Errorf("--help: %d\n%s", code, out)
	}
	for _, args := range [][]string{{"--version"}, {"version"}} {
		if code, out := cli(nil, args...); code != 0 || out != "go-microsip 1.2.3 (abc1234)\n" {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
	if code, _ := cli(nil, "-h"); code != 0 {
		t.Errorf("-h: %d", code)
	}
}

func TestDoctorReportsWithoutRevealingThePassword(t *testing.T) {
	file := config(t)
	for _, args := range [][]string{{"--config", file, "doctor"}, {"doctor", "--json", "--config=" + file}} {
		code, out := cli(map[string]string{"GO_MICROSIP_SIP_PASSWORD": marker}, args...)
		if code != 0 || strings.Contains(out, marker) || !strings.Contains(out, "1001") {
			t.Errorf("%v: %d\n%s", args, code, out)
		}
		if args[1] == "--json" && (!strings.HasPrefix(out, "{") || !strings.Contains(out, `"password_present": true`) || !strings.Contains(out, `"version": "1.2.3"`)) {
			t.Errorf("JSON report:\n%s", out)
		}
		if args[1] != "--json" && (!strings.Contains(out, "configuration: OK") || !strings.Contains(out, "password: present [env]")) {
			t.Errorf("text report:\n%s", out)
		}
	}
	// The report of a configuration that does not resolve is still a report, with status 0.
	if code, out := cli(nil, "doctor"); code != 0 || !strings.Contains(out, "configuration: INVALID") || !strings.Contains(out, "account.domain") {
		t.Errorf("doctor without an account: %d\n%s", code, out)
	}
	if code, out := cli(map[string]string{"RUST_MICROSIP_ACCOUNT_DOMAIN": "pbx.invalid"}, "doctor"); code != 0 || !strings.Contains(out, "RUST_MICROSIP_ACCOUNT_DOMAIN") {
		t.Errorf("legacy variables: %d\n%s", code, out)
	}
}

func TestValidateAndExportUseTheEffectiveConfiguration(t *testing.T) {
	file := config(t)
	if code, out := cli(nil, "--config", file, "config", "validate"); code != 0 || out != "configuration: OK\n" {
		t.Errorf("validate: %d %q", code, out)
	}
	code, out := cli(map[string]string{"GO_MICROSIP_SIP_PASSWORD": marker, "GO_MICROSIP_UI_THEME": "win98"}, "--config", file, "config", "export")
	if code != 0 || !strings.Contains(out, `username = "1001"`) || !strings.Contains(out, `theme = "win98"`) || strings.Contains(out, marker) {
		t.Errorf("export: %d\n%s", code, out)
	}
	if code, out := cli(nil, "config", "validate"); code != 1 || !strings.HasPrefix(out, "configuration: INVALID\nerror: missing required setting") {
		t.Errorf("validate without an account: %d %q", code, out)
	}
	if code, _ := cli(nil, "--config", "/nonexistent/go-microsip.toml", "config", "export"); code != 1 {
		t.Errorf("export of a missing file: %d", code)
	}
}

func TestInvalidArgumentsAreUsageErrorsWithoutEcho(t *testing.T) {
	for _, args := range [][]string{
		{"--bogus"}, {marker}, {"--password", marker}, {"--password=" + marker}, {"doctor", marker}, {"config", marker},
		{"config", "validate", "--apply"}, {"config", "import"}, {"run", "--hangup-after", marker}, {"\xff\xfe"},
		{"--config"}, {"doctor", "--sip-pass=" + marker},
	} {
		if code, out := cli(nil, args...); code != 2 || strings.Contains(out, marker) || !strings.HasPrefix(out, "go-microsip: ") {
			t.Errorf("%q: %d %q", args, code, out)
		}
	}
}

func TestConfigImportPreviewsThenAppliesIntoSQLiteOnly(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	content := "schema = 1\n[account]\nusername = \"1009\"\ndomain = \"pbx.invalid\"\npassword = \"" + marker + "\"\n[ui]\ntheme = \"dark\"\n"
	file := write(t, filepath.Join(dir, "import.toml"), content)
	env := map[string]string{"XDG_DATA_HOME": data}

	// Preview: sanitized, nothing written, no database created.
	code, out := cli(env, "config", "import", file)
	if code != 0 || !strings.Contains(out, `account.username = "1009"`) || !strings.Contains(out, "--apply") ||
		!strings.Contains(out, "literal password is not imported") || strings.Contains(out, marker) {
		t.Fatalf("preview: %d\n%s", code, out)
	}
	if _, err := os.Stat(data); err == nil {
		t.Fatal("the preview created the data directory")
	}
	// Apply: stored in SQLite in one transaction; the file is untouched.
	code, out = cli(env, "config", "import", file, "--apply")
	if code != 0 || !strings.Contains(out, "Imported into the local database") || strings.Contains(out, marker) {
		t.Fatalf("apply: %d\n%s", code, out)
	}
	if after, _ := os.ReadFile(file); string(after) != content {
		t.Error("the imported file was modified")
	}
	db := filepath.Join(data, "go-microsip", "go-microsip.sqlite3")
	info, err := os.Stat(db)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("database: %v, %v", info, err)
	}
	if raw, _ := os.ReadFile(db); bytes.Contains(raw, []byte(marker)) {
		t.Error("the password is in the database")
	}
	// The stored settings are now part of the effective configuration.
	code, out = cli(env, "config", "export")
	if code != 0 || !strings.Contains(out, `username = "1009"`) || !strings.Contains(out, `theme = "dark"`) || strings.Contains(out, marker) {
		t.Errorf("export after the import: %d\n%s", code, out)
	}
	if _, out := cli(env, "doctor"); !strings.Contains(out, `account.username = "1009" [sqlite]`) {
		t.Errorf("doctor after the import:\n%s", out)
	}
	// Higher sources still win over the database.
	if _, out := cli(map[string]string{"XDG_DATA_HOME": data, "GO_MICROSIP_UI_THEME": "light"}, "config", "export"); !strings.Contains(out, `theme = "light"`) {
		t.Errorf("the environment did not win:\n%s", out)
	}
}

func TestConfigImportRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	env := map[string]string{"XDG_DATA_HOME": data}
	bad := write(t, filepath.Join(dir, "bad.toml"), "schema = 1\n[account]\nbogus = 1\n")
	if code, out := cli(env, "config", "import", bad, "--apply"); code != 1 || !strings.Contains(out, "unknown key") {
		t.Errorf("unknown key: %d %q", code, out)
	}
	if code, _ := cli(env, "config", "import", "/nonexistent/go-microsip.toml"); code != 1 {
		t.Errorf("missing file: %d", code)
	}
	if _, err := os.Stat(data); err == nil {
		t.Error("a refused import created the data directory")
	}
	// Without a data directory the preview is still shown and nothing is imported.
	good := write(t, filepath.Join(dir, "good.toml"), "schema = 1\n[account]\nusername = \"1009\"\ndomain = \"pbx.invalid\"\n")
	if code, out := cli(nil, "config", "import", good, "--apply"); code != 1 || !strings.Contains(out, "import preview") || !strings.Contains(out, "nothing imported") {
		t.Errorf("no data directory: %d %q", code, out)
	}
}

func TestMalformedFilesNeverEchoSecretLines(t *testing.T) {
	dir := t.TempDir()
	// Unterminated strings: a parser's own message would quote these lines.
	toml := write(t, filepath.Join(dir, "bad.toml"), "schema = 1\n[account]\nusername = \"1001\"\npassword = \""+marker+"\n")
	dotenv := write(t, filepath.Join(dir, "bad.env"), "GO_MICROSIP_SIP_PASSWORD=\""+marker+"\n")
	for _, args := range [][]string{
		{"--config", toml, "config", "validate"}, {"--config", toml, "doctor"}, {"--config", toml, "config", "export"},
		{"--env-file", dotenv, "config", "validate"}, {"--env-file", dotenv, "doctor", "--json"}, {"config", "import", toml},
		{"--config", toml, "run"},
	} {
		code, out := cli(map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}, args...)
		if strings.Contains(out, marker) || !strings.Contains(out, "line") {
			t.Errorf("%v: %d\n%s", args, code, out)
		}
	}
}

func TestRunRefusesWhatItCannotStart(t *testing.T) {
	file := config(t)
	data := map[string]string{"XDG_DATA_HOME": t.TempDir()}
	if code, out := cli(data, "run"); code != 1 || !strings.HasPrefix(out, "configuration: INVALID") {
		t.Errorf("no account: %d %q", code, out)
	}
	if code, out := cli(data, "--config", file, "run"); code != 1 || !strings.Contains(out, "no password is available") {
		t.Errorf("no password: %d %q", code, out)
	}
	data["GO_MICROSIP_SIP_PASSWORD"] = marker
	if code, out := cli(data, "--config", file, "run", "--call", "sip:"+marker+"@x"); code != 1 || strings.Contains(out, marker) || !strings.Contains(out, "number to call is not valid") {
		t.Errorf("invalid number: %d %q", code, out)
	}
	if entries, _ := os.ReadDir(data["XDG_DATA_HOME"]); len(entries) != 0 {
		t.Error("a refused run created the database")
	}
}
