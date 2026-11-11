package config

// These tests are the Rust candidate's crates/config/src/tests.rs, ported one for one, with the
// names this application uses (GO_MICROSIP_, go-microsip) and the two documented differences: an
// unknown theme resolves to "system" instead of failing, and the argument parser is Cobra's, so
// only the password-argument rule is tested here.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/domain"
)

const (
	secret   = "s3cret-marker-9f2"
	baseTOML = "schema = 1\n[account]\nusername = \"1001\"\ndomain = \"pbx.invalid\"\n"
	me       = 1000 // the fake user every test file belongs to
)

// access is a fake ports.FileAccess: real permission bits, a fixed owner.
type access struct{ user uint32 }

func (a access) CurrentUser() uint32 { return a.user }
func (access) Inspect(info os.FileInfo) (owner, mode uint32) {
	return me, uint32(info.Mode().Perm())
}

func ctx() Context {
	return Context{Env: map[string]string{}, Flags: map[Field]string{}, Files: access{me}}
}

// file writes a file with exactly this mode and returns its path.
func file(t *testing.T, dir, name, text string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func ok(t *testing.T, c Context) *Effective {
	t.Helper()
	cfg, err := Resolve(c)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	return cfg
}

// fail resolves and returns the configuration error it must end with.
func fail(t *testing.T, c Context) *Error {
	t.Helper()
	_, err := Resolve(c)
	return asError(t, err)
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("got %v, want a configuration error", err)
	}
	return e
}

func TestEveryPrecedenceCombinationPicksTheHighestSource(t *testing.T) {
	dir := t.TempDir()
	for mask := 0; mask < 32; mask++ {
		has := func(bit int) bool { return mask&(1<<bit) != 0 }
		c := ctx()
		text := baseTOML
		if has(0) {
			text += "server = \"toml.invalid\"\n[ui]\ntheme = \"light\"\n"
		}
		c.ConfigPath = file(t, dir, "c.toml", text, 0o600)
		if has(1) {
			c.EnvFile = file(t, dir, "e.env", "GO_MICROSIP_ACCOUNT_SERVER=dotenv.invalid\nGO_MICROSIP_UI_THEME=dark\n", 0o600)
		}
		if has(2) {
			c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_SERVER": "env.invalid", "GO_MICROSIP_UI_THEME": "system"}
		}
		if has(3) {
			c.Flags[Server], c.Flags[UITheme] = "flag.invalid", "light"
		}
		if has(4) {
			c.SQLite = SQLiteLayer(map[string]string{"account.server": "sqlite.invalid", "ui.theme": "dark", "not.a_key": "ignored"})
		}
		cfg := ok(t, c)
		server, theme, source := "pbx.invalid", ThemeDark, Default
		switch {
		case has(3):
			server, theme, source = "flag.invalid", ThemeLight, Flag
		case has(2):
			server, theme, source = "env.invalid", ThemeSystem, Env
		case has(1):
			server, theme, source = "dotenv.invalid", ThemeDark, DotEnv
		case has(0):
			server, theme, source = "toml.invalid", ThemeLight, TOML
		case has(4):
			server, theme, source = "sqlite.invalid", ThemeDark, SQLite
		}
		if cfg.Config.Account.Server != server || cfg.Config.UI.Theme != theme {
			t.Errorf("mask %d: server %q theme %q, want %q %q", mask, cfg.Config.Account.Server, cfg.Config.UI.Theme, server, theme)
		}
		if cfg.Source(Server) != source || cfg.Source(UITheme) != source || cfg.Source(Username) != TOML {
			t.Errorf("mask %d: sources %v %v %v, want %v %v toml", mask, cfg.Source(Server), cfg.Source(UITheme), cfg.Source(Username), source, source)
		}
	}
}

func TestDefaultsFollowTransportAndDomain(t *testing.T) {
	c := ctx()
	c.Env = map[string]string{
		"GO_MICROSIP_ACCOUNT_USERNAME": "1001", "GO_MICROSIP_ACCOUNT_DOMAIN": "pbx.invalid",
		"GO_MICROSIP_ACCOUNT_TRANSPORT": "tls", "GO_MICROSIP_ACCOUNT_MEDIA": "srtp", "GO_MICROSIP_UI_DND": "1",
	}
	cfg := ok(t, c)
	a := cfg.Config.Account
	if a.Port != 5061 || a.Server != "pbx.invalid" || a.Expires != DefaultExpires {
		t.Errorf("port %d server %q expires %d", a.Port, a.Server, a.Expires)
	}
	if cfg.Config.PasswordEnv != DefaultPasswordEnv || !cfg.Config.UI.DND || cfg.PasswordSource != "" || cfg.ConfigFile != "" {
		t.Errorf("password env %q dnd %v password %q file %q", cfg.Config.PasswordEnv, cfg.Config.UI.DND, cfg.PasswordSource, cfg.ConfigFile)
	}
	if cfg.Source(Port) != Default {
		t.Errorf("port source %v", cfg.Source(Port))
	}
}

func TestMissingAndInvalidValuesAreDistinguished(t *testing.T) {
	if e := fail(t, ctx()); e.Kind != Missing || e.Field != Domain {
		t.Errorf("got %v, want missing account.domain", e)
	}
	c := ctx()
	c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_DOMAIN": "pbx.invalid"}
	if e := fail(t, c); e.Kind != Missing || e.Field != Username {
		t.Errorf("got %v, want missing account.username", e)
	}

	c.ConfigPath = file(t, t.TempDir(), "c.toml", baseTOML+"port = 5070\n", 0o600)
	for key, value := range map[string]string{
		"GO_MICROSIP_ACCOUNT_PORT":         "70000",
		"GO_MICROSIP_ACCOUNT_TRANSPORT":    "UDP",
		"GO_MICROSIP_ACCOUNT_EXPIRES":      "0",
		"GO_MICROSIP_UI_DND":               "maybe",
		"GO_MICROSIP_ACCOUNT_CA_FILE":      "relative.pem",
		"GO_MICROSIP_ACCOUNT_PASSWORD_ENV": "1BAD",
	} {
		c.Env = map[string]string{key: value}
		// An invalid higher source is an error, never a silent fall-back to TOML.
		if e := fail(t, c); e.Kind != Invalid || e.Origin != Env {
			t.Errorf("%s: got %v, want invalid from env", key, e)
		}
	}
	c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_SERVER": "evil host"}
	if e := fail(t, c); e.Kind != Validation || !errors.Is(e, domain.ErrInvalidCharacters) {
		t.Errorf("got %v, want invalid characters", e)
	}
	c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_MEDIA": "srtp"}
	if e := fail(t, c); e.Kind != Validation || !errors.Is(e, domain.ErrSRTPRequiresTLS) {
		t.Errorf("got %v, want SRTP requires TLS", e)
	}
}

func TestTOMLSchemaTypesAndUnknownKeysAreRejected(t *testing.T) {
	dir := t.TempDir()
	for text, want := range map[string]Error{
		"[account]\nusername = \"1\"\n":               {Kind: UnsupportedSchema},
		"schema = 2\n":                                {Kind: UnsupportedSchema},
		"schema = 1\nextra = 1\n":                     {Kind: UnknownKey, Key: "extra"},
		"schema = 1\n[telephony]\n":                   {Kind: UnknownKey, Key: "telephony"},
		"schema = 1\n[account]\nfoo = 1\n":            {Kind: UnknownKey, Key: "account.foo"},
		"schema = 1\n[account]\nport = \"5060\"\n":    {Kind: WrongType, Key: "account.port"},
		"schema = 1\n[account]\nusername = 1001\n":    {Kind: WrongType, Key: "account.username"},
		"schema = 1\n[account]\npassword = 1\n":       {Kind: WrongType, Key: "account.password"},
		"schema = 1\naccount = 1\n":                   {Kind: WrongType, Key: "account"},
		"schema = 1\n[account\n":                      {Kind: TOMLSyntax, Line: 2},
		"schema = 1\n[account]\n\"a b\" = 1\n":        {Kind: UnknownKey, Key: "<unprintable key>"},
		"schema = 1\n[ui]\ndnd = \"true\"\n":          {Kind: WrongType, Key: "ui.dnd"},
		"schema = 1\n[account]\nexpires = 3.5\n":      {Kind: WrongType, Key: "account.expires"},
		"schema = 1\n[audio]\ninput_device = true\n":  {Kind: WrongType, Key: "audio.input_device"},
		"schema = \"1\"\n[account]\nusername = \"1\"": {Kind: UnsupportedSchema},
	} {
		c := ctx()
		c.ConfigPath = file(t, dir, "c.toml", text, 0o600)
		e := fail(t, c)
		if e.Kind != want.Kind || e.Key != want.Key || e.Line != want.Line {
			t.Errorf("%q: got kind %d key %q line %d (%v)", text, e.Kind, e.Key, e.Line, e)
		}
	}
}

func TestExplicitFilesMustExistButTheDefaultIsOptional(t *testing.T) {
	dir := t.TempDir()
	c := ctx()
	c.ConfigPath = filepath.Join(dir, "absent.toml")
	if e := fail(t, c); e.Kind != FileMissing {
		t.Errorf("--config: %v", e)
	}
	c.ConfigPath, c.EnvFile = "", filepath.Join(dir, "absent.env")
	if e := fail(t, c); e.Kind != FileMissing {
		t.Errorf("--env-file: %v", e)
	}

	base := map[string]string{"GO_MICROSIP_ACCOUNT_USERNAME": "1", "GO_MICROSIP_ACCOUNT_DOMAIN": "d.invalid"}
	c = ctx()
	c.Env = maps.Clone(base)
	c.Env["XDG_CONFIG_HOME"] = dir
	if cfg := ok(t, c); cfg.ConfigFile != "" {
		t.Errorf("a missing default file was reported as loaded: %q", cfg.ConfigFile)
	}
	path := file(t, dir, "go-microsip/config.toml", "schema = 1\n[ui]\ntheme = \"light\"\n", 0o644)
	if cfg := ok(t, c); cfg.ConfigFile != path || cfg.Config.UI.Theme != ThemeLight {
		t.Errorf("default file: %q theme %q", cfg.ConfigFile, cfg.Config.UI.Theme)
	}
	// The HOME fall-back, and a relative XDG_CONFIG_HOME being ignored.
	c.Env = maps.Clone(base)
	c.Env["XDG_CONFIG_HOME"], c.Env["HOME"] = "relative", dir
	if got, want := c.DefaultConfigPath(), filepath.Join(dir, ".config/go-microsip/config.toml"); got != want {
		t.Errorf("default path %q, want %q", got, want)
	}
	if c.Env = base; c.DefaultConfigPath() != "" {
		t.Errorf("a default path without HOME: %q", c.DefaultConfigPath())
	}
	// An explicit --config replaces the default instead of merging with it.
	c.Env = maps.Clone(base)
	c.Env["XDG_CONFIG_HOME"] = dir
	c.ConfigPath = file(t, dir, "other.toml", "schema = 1\n", 0o600)
	if cfg := ok(t, c); cfg.Config.UI.Theme != ThemeDark {
		t.Errorf("theme %q: the default file was merged in", cfg.Config.UI.Theme)
	}
}

func TestWritableOrSecretBearingFilesNeedSafePermissions(t *testing.T) {
	dir := t.TempDir()
	insecure := func(e *Error, problem FileProblem) bool { return e.Kind == InsecureFile && e.Problem == problem }
	c := ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML, 0o666)
	if e := fail(t, c); !insecure(e, Writable) {
		t.Errorf("world-writable TOML: %v", e)
	}
	c.ConfigPath = file(t, dir, "c.toml", baseTOML, 0o644)
	if cfg := ok(t, c); cfg.PasswordSource != "" {
		t.Errorf("a password out of nowhere: %q", cfg.PasswordSource)
	}

	literal := baseTOML + "password = \"" + secret + "\"\n"
	c.ConfigPath = file(t, dir, "c.toml", literal, 0o640)
	e := fail(t, c)
	if !insecure(e, NotPrivate) || strings.Contains(e.Error(), secret) || !strings.Contains(e.Error(), "chmod 600") {
		t.Errorf("group-readable literal password: %v", e)
	}
	c.ConfigPath = file(t, dir, "c.toml", literal, 0o600)
	if cfg := ok(t, c); !cfg.LiteralPasswordWarning || cfg.PasswordSource != PasswordFromLiteral || cfg.Password.Reveal() != secret {
		t.Errorf("literal password: warning %v source %q", cfg.LiteralPasswordWarning, cfg.PasswordSource)
	}
	// A file owned by someone else is refused before its contents are used.
	c.Files = access{me + 1}
	if e := fail(t, c); !insecure(e, ForeignOwner) {
		t.Errorf("foreign TOML: %v", e)
	}

	pem := file(t, dir, "ca.pem", "-----BEGIN CERTIFICATE-----\n", 0o644)
	if _, err := ReadCAFile(pem, access{me}); err != nil {
		t.Errorf("CA bundle: %v", err)
	}
	if _, err := ReadCAFile(pem, access{me + 1}); !insecure(asError(t, err), ForeignOwner) {
		t.Errorf("foreign CA bundle: %v", err)
	}
	writable := file(t, dir, "ca-w.pem", "-----BEGIN CERTIFICATE-----\n", 0o666)
	if _, err := ReadCAFile(writable, access{me}); !insecure(asError(t, err), Writable) {
		t.Errorf("writable CA bundle: %v", err)
	}
	if _, err := ReadCAFile(filepath.Join(dir, "absent.pem"), access{me}); asError(t, err).Kind != FileMissing {
		t.Errorf("absent CA bundle: %v", err)
	}

	c = ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML, 0o600)
	c.EnvFile = file(t, dir, "e.env", "GO_MICROSIP_UI_THEME=dark\n", 0o644)
	if cfg := ok(t, c); cfg.PasswordSource != "" {
		t.Errorf("a password out of nowhere: %q", cfg.PasswordSource)
	}
	c.EnvFile = file(t, dir, "e.env", "GO_MICROSIP_SIP_PASSWORD="+secret+"\n", 0o644)
	if e := fail(t, c); !insecure(e, NotPrivate) {
		t.Errorf("readable .env with a password: %v", e)
	}
}

func TestPasswordFileMustBePrivateRegularAndNotASymlink(t *testing.T) {
	dir := t.TempDir()
	c := ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML, 0o600)
	pw := file(t, dir, "pw", secret+"\r\n", 0o600)
	c.Flags[PasswordFile] = pw
	if cfg := ok(t, c); cfg.Password.Reveal() != secret || cfg.PasswordSource != PasswordFromFile {
		t.Errorf("password file: source %q", cfg.PasswordSource)
	}
	if err := os.Chmod(pw, 0o604); err != nil {
		t.Fatal(err)
	}
	if e := fail(t, c); e.Kind != InsecureFile || e.Problem != NotPrivate {
		t.Errorf("readable password file: %v", e)
	}
	if err := os.Chmod(pw, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(pw, link); err != nil {
		t.Fatal(err)
	}
	c.Flags[PasswordFile] = link
	if e := fail(t, c); e.Kind != InsecureFile || e.Problem != Symlink {
		t.Errorf("symlinked password file: %v", e)
	}
	c.Flags[PasswordFile] = dir
	if e := fail(t, c); e.Kind != InsecureFile || e.Problem != NotRegular {
		t.Errorf("directory as password file: %v", e)
	}
	c.Flags[PasswordFile] = file(t, dir, "empty", "\n", 0o600)
	if e := fail(t, c); e.Kind != EmptyPassword {
		t.Errorf("empty password file: %v", e)
	}
	c.Flags[PasswordFile] = file(t, dir, "big", strings.Repeat("x", maxSecretBytes+1), 0o600)
	if e := fail(t, c); e.Kind != FileTooLarge {
		t.Errorf("oversized password file: %v", e)
	}
	c.Flags[PasswordFile] = file(t, dir, "binary", "\xff\xfe", 0o600)
	if e := fail(t, c); e.Kind != NotUTF8 {
		t.Errorf("binary password file: %v", e)
	}
}

func TestPasswordSourcesFollowTheDocumentedOrder(t *testing.T) {
	dir := t.TempDir()
	c := ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML+"password_env = \"LAB_PW\"\npassword = \"literal\"\n", 0o600)
	c.Flags[PasswordFile] = file(t, dir, "pw", "file", 0o600)
	c.EnvFile = file(t, dir, "e.env", "LAB_PW=dotenv\n", 0o600)
	c.Env = map[string]string{"LAB_PW": "env"}
	for _, want := range []PasswordSource{PasswordFromEnv, PasswordFromDotEnv, PasswordFromFile, PasswordFromLiteral} {
		cfg := ok(t, c)
		value := map[PasswordSource]string{PasswordFromEnv: "env", PasswordFromDotEnv: "dotenv", PasswordFromFile: "file", PasswordFromLiteral: "literal"}[want]
		if cfg.PasswordSource != want || cfg.Password.Reveal() != value {
			t.Errorf("source %q, want %q", cfg.PasswordSource, want)
		}
		switch want { // remove the source just used: the next one takes over
		case PasswordFromEnv:
			c.Env = map[string]string{}
		case PasswordFromDotEnv:
			c.EnvFile = ""
		default:
			c.Flags = map[Field]string{}
		}
	}
}

func TestSecretsNeverAppearInFormattingErrorsExportsOrDoctor(t *testing.T) {
	dir := t.TempDir()
	c := ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML+"password = \""+secret+"\"\n", 0o600)
	cfg := ok(t, c)
	report := Doctor(c)
	export, err := cfg.Config.TOML()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		fmt.Sprintf("%v %+v %#v %s %q %x", cfg, cfg, cfg, cfg.Password, cfg.Password, cfg.Password),
		fmt.Sprintf("%+v %#v", *cfg, *cfg), export, report.JSON(), report.String(),
	} {
		if strings.Contains(text, secret) {
			t.Errorf("the password leaked: %s", text)
		}
	}
	if j := report.JSON(); !strings.Contains(j, `"password_present":true`) || !strings.Contains(j, `"password_source":"toml_literal"`) {
		t.Errorf("doctor JSON: %s", j)
	}
	if !strings.Contains(report.String(), "password: present [toml_literal]") || !strings.Contains(report.String(), "warning: literal password") {
		t.Errorf("doctor text: %s", report)
	}

	// Values never leak through error messages.
	c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_PORT": secret}
	e := fail(t, c)
	if text := e.Error() + fmt.Sprintf("%+v %#v", e, e); strings.Contains(text, secret) {
		t.Errorf("the value leaked: %s", text)
	}
	if failure := Doctor(c); failure.OK || !strings.HasPrefix(failure.JSON(), `{"ok":false,"error":"invalid value`) ||
		!strings.HasPrefix(failure.String(), "configuration: INVALID\nerror: invalid value") {
		t.Errorf("doctor of a failure: %s", failure.JSON())
	}
	c.Env = map[string]string{}
	for _, bad := range []string{
		"schema = 1\n[account]\nport = \"" + secret + "\"\n",
		"schema = 1\nx = \"" + secret + "\n",
		"schema = 1\n[account]\n" + secret + " = 1\n",
	} {
		c.ConfigPath = file(t, dir, "c.toml", bad, 0o600)
		e := fail(t, c)
		if text := e.Error() + fmt.Sprintf("%+v %#v", e, e); strings.Contains(text, secret) && e.Kind != UnknownKey {
			t.Errorf("the value leaked: %s", text) // a key is named; a value never is
		}
	}
}

func TestAtomicSaveRoundTripsWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	c := ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML+"display_name = \"Lab Désk\"\ntransport = \"tls\"\nmedia = \"srtp\"\n"+
		"ca_file = \"/etc/lab-ca.pem\"\npassword = \""+secret+"\"\n[ui]\ndnd = true\ntheme = \"win98\"\n[audio]\ninput_device = \"USB\"\n", 0o600)
	original := ok(t, c).Config

	target := filepath.Join(dir, "nested/deeper/config.toml")
	if err := Save(original, target); err != nil {
		t.Fatalf("save: %v", err)
	}
	mode := func(path string) os.FileMode {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	if mode(target) != 0o600 || mode(filepath.Dir(target)) != 0o700 {
		t.Errorf("modes: file %o directory %o", mode(target), mode(filepath.Dir(target)))
	}
	saved, _ := os.ReadFile(target)
	if strings.Contains(string(saved), secret) || strings.Contains(string(saved), "password =") {
		t.Errorf("the saved file holds a password:\n%s", saved)
	}
	if entries, _ := os.ReadDir(filepath.Dir(target)); len(entries) != 1 {
		t.Errorf("%d entries beside the saved file: a temporary file was left behind", len(entries))
	}

	// Overwrite in place, then reload.
	if err := Save(original, target); err != nil {
		t.Fatalf("save again: %v", err)
	}
	c.ConfigPath = target
	reloaded := ok(t, c)
	if reloaded.Config != original {
		t.Errorf("reloaded %+v, saved %+v", reloaded.Config, original)
	}
	if reloaded.LiteralPasswordWarning || reloaded.PasswordSource != "" || reloaded.Config.Account.DisplayName != "Lab Désk" || reloaded.Config.UI.Theme != ThemeWin98 {
		t.Errorf("reloaded: warning %v password %q name %q theme %q", reloaded.LiteralPasswordWarning, reloaded.PasswordSource,
			reloaded.Config.Account.DisplayName, reloaded.Config.UI.Theme)
	}
	// A failed save leaves the previous file as it was.
	if err := Save(original, filepath.Join(target, "under-a-file.toml")); asError(t, err).Kind != IO {
		t.Errorf("save under a file: %v", err)
	}
	if again, _ := os.ReadFile(target); string(again) != string(saved) {
		t.Error("a failed save changed the previous file")
	}
}

// The export is stable text: sections and keys in order, strings double-quoted and escaped so that
// what is written is read back unchanged.
func TestExportIsOrderedQuotedAndRoundTrips(t *testing.T) {
	stored := Stored{
		Account:     domain.AccountSettings{DisplayName: "Lab Désk", Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Expires: 300},
		PasswordEnv: DefaultPasswordEnv, UI: UI{Theme: ThemeXP}, Audio: Audio{OutputDevice: `Speaker "A" \ 'B'`},
	}
	text, err := stored.TOML()
	if err != nil {
		t.Fatal(err)
	}
	want := `schema = 1

[account]
display_name = "Lab Désk"
domain = "pbx.invalid"
expires = 300
media = "rtp"
password_env = "GO_MICROSIP_SIP_PASSWORD"
port = 5060
server = "pbx.invalid"
transport = "udp"
username = "1001"

[audio]
output_device = "Speaker \"A\" \\ 'B'"

[ui]
dnd = false
theme = "xp"
`
	if text != want {
		t.Errorf("export:\n%s\nwant:\n%s", text, want)
	}
	c := ctx()
	c.ConfigPath = file(t, t.TempDir(), "c.toml", text, 0o600)
	if back := ok(t, c).Config; back != stored {
		t.Errorf("read back %+v, wrote %+v", back, stored)
	}
	if got := tomlString("a\tb\nc\x01\x7f"); got != `"a\tb\nc\u0001\u007F"` {
		t.Errorf("control characters: %s", got)
	}
	stored.CAFile = "/etc/\xff.pem"
	if _, err := stored.TOML(); asError(t, err).Kind != Serialize {
		t.Errorf("a path that is not text: %v", err)
	}
}

func TestDotEnvParsingEdgeCases(t *testing.T) {
	text := "# comment\r\n" +
		"\r\n" +
		"export A=plain value  # trailing\r\n" +
		"B='single # kept'\n" +
		"C=\"dq \\\"x\\\" \\\\ \\n\" # c\n" +
		"D=a#b\n" +
		"E=\n" +
		"F = spaced\n" +
		"G=\"é ü\"\n" +
		"A=last\n"
	got, err := ParseDotEnv(text)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "last", "B": "single # kept", "C": "dq \"x\" \\ \n", "D": "a#b", "E": "", "F": "spaced", "G": "é ü"}
	if !maps.Equal(got, want) {
		t.Errorf("parsed %q, want %q", got, want)
	}
	for bad, line := range map[string]int{
		"A=1\nnoequals\n": 2, "1A=x": 1, "A-B=x": 1, "A=\"open": 1, "A='open": 1, "A=\"x\" junk": 1, "A=\"\\q\"": 1, "A=\"x\\": 1, "=x": 1,
	} {
		var syntax *DotEnvError
		if _, err := ParseDotEnv(bad); !errors.As(err, &syntax) || syntax.Line != line {
			t.Errorf("%q: got %v, want a syntax error at line %d", bad, err, line)
		}
	}

	dir := t.TempDir()
	c := ctx()
	c.ConfigPath = file(t, dir, "c.toml", baseTOML, 0o600)
	c.EnvFile = file(t, dir, "e.env", strings.Repeat("#", maxConfigBytes+1), 0o600)
	if e := fail(t, c); e.Kind != FileTooLarge {
		t.Errorf("oversized .env: %v", e)
	}
	c.EnvFile = file(t, dir, "e.env", "X="+secret+"\n\"bad", 0o600)
	if e := fail(t, c); e.Kind != DotEnvSyntax || e.Line != 2 || strings.Contains(e.Error(), secret) {
		t.Errorf(".env syntax error: %v", e)
	}
}

// The Rust parser refused any option whose name contains "pass" before looking at anything else.
func TestPasswordLikeArgumentsAreRefused(t *testing.T) {
	for _, args := range [][]string{
		{"--password", secret}, {"--sip-pass=" + secret}, {"doctor", "--PASSWORD=" + secret}, {"-pass", secret},
		{"--config", "/x/c.toml", "--passwd", secret}, {"-p4ssword-PASS"},
	} {
		err := CheckArguments(args)
		if !errors.Is(err, ErrPasswordNotAccepted) {
			t.Errorf("%q: got %v, want the refusal", args[0], err)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("the refusal echoes the argument: %v", err)
		}
	}
	for _, args := range [][]string{
		nil, {"--config", "/x/password.toml", "--env-file=/y/pass.env", "doctor", "--json"}, {"config", "import", "/x/passwords.toml"},
		{"--", "--password"},
	} {
		if err := CheckArguments(args); err != nil {
			t.Errorf("%q: refused: %v", args, err)
		}
	}
}

func TestSQLiteLayerAloneResolvesAndInvalidValuesNameTheOrigin(t *testing.T) {
	c := ctx()
	c.SQLite = SQLiteLayer(map[string]string{"account.username": "1001", "account.domain": "pbx.invalid", "account.port": "5070", "ui.dnd": "true"})
	cfg := ok(t, c)
	if cfg.Config.Account.Port != 5070 || !cfg.Config.UI.DND || cfg.Source(Domain) != SQLite || cfg.Source(Expires) != Default {
		t.Errorf("port %d dnd %v sources %v %v", cfg.Config.Account.Port, cfg.Config.UI.DND, cfg.Source(Domain), cfg.Source(Expires))
	}
	report := Doctor(c)
	if !strings.Contains(report.JSON(), `{"key":"account.domain","value":"pbx.invalid","source":"sqlite"}`) {
		t.Errorf("doctor JSON: %s", report.JSON())
	}
	if !strings.Contains(report.String(), "account.port = 5070 [sqlite]") || !strings.Contains(report.String(), `account.domain = "pbx.invalid" [sqlite]`) ||
		!strings.Contains(report.String(), "account.ca_file = (unset) [default]") {
		t.Errorf("doctor text: %s", report)
	}

	for field, value := range map[Field]string{Port: "70000x", Transport: "carrier-pigeon", PasswordFile: "relative/path"} {
		c.SQLite[field] = value
		e := fail(t, c)
		if e.Kind != Invalid || e.Field != field || e.Origin != SQLite {
			t.Errorf("%v: got %v", field, e)
		}
		if text := e.Error(); !strings.Contains(text, field.Key()) || !strings.Contains(text, "sqlite") || strings.Contains(text, value) {
			t.Errorf("%v: message %q", field, text)
		}
		delete(c.SQLite, field)
	}
	// A skin this version does not know falls back to the desktop's choice instead of failing.
	c.SQLite[UITheme] = "neon"
	if cfg := ok(t, c); cfg.Config.UI.Theme != ThemeSystem || cfg.Source(UITheme) != SQLite {
		t.Errorf("unknown theme: %q from %v", cfg.Config.UI.Theme, cfg.Source(UITheme))
	}
	// A higher source hides the stored value.
	c.Flags[UITheme] = "ubuntu"
	if cfg := ok(t, c); cfg.Config.UI.Theme != ThemeUbuntu || cfg.Source(UITheme) != Flag {
		t.Errorf("theme %q from %v", cfg.Config.UI.Theme, cfg.Source(UITheme))
	}
	if len(Themes()) != 7 || ParseTheme("xp") != ThemeXP || ParseTheme("macos") != ThemeMacOS || ParseTheme("") != ThemeSystem {
		t.Errorf("themes: %v", Themes())
	}
}

// edits renders SQLiteEdits' result for comparison: "<nil>" marks a removal.
func edits(t *testing.T, sources map[Field]Source, before *Stored, after Stored) map[string]string {
	t.Helper()
	got, err := SQLiteEdits(sources, before, after)
	if err != nil {
		t.Fatalf("edits: %v", err)
	}
	out := map[string]string{}
	for k, v := range got {
		if v == nil {
			out[k] = "<nil>"
		} else {
			out[k] = *v
		}
	}
	return out
}

func TestEditsSkipUnchangedAndRefuseOverriddenFields(t *testing.T) {
	c := ctx()
	stored := map[string]string{"account.username": "1001", "account.domain": "pbx.invalid"}
	c.SQLite = SQLiteLayer(stored)
	c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_SERVER": "env.invalid"}
	cfg := ok(t, c)
	before := cfg.Config

	// Unchanged: nothing to write, and the environment's override is never written back.
	if got := edits(t, cfg.Provenance(), &before, before); len(got) != 0 {
		t.Errorf("unchanged form: %v", got)
	}
	after := before
	after.UI.Theme, after.Account.Port, after.Audio.InputDevice = ThemeLight, 5070, "Mic"
	want := map[string]string{"account.port": "5070", "audio.input_device": "Mic", "ui.theme": "light"}
	if got := edits(t, cfg.Provenance(), &before, after); !maps.Equal(got, want) {
		t.Errorf("edits %v, want %v", got, want)
	}
	// Clearing an optional stored value removes it.
	cleared := after
	cleared.Audio.InputDevice = ""
	if got := edits(t, cfg.Provenance(), &after, cleared); !maps.Equal(got, map[string]string{"audio.input_device": "<nil>"}) {
		t.Errorf("cleared device: %v", got)
	}
	// Changing a field fixed by the environment is refused, naming the source.
	after.Account.Server = "ui.invalid"
	_, err := SQLiteEdits(cfg.Provenance(), &before, after)
	if e := asError(t, err); e.Kind != Overridden || e.Field != Server || e.Origin != Env ||
		!strings.Contains(e.Error(), "set by env") || strings.Contains(e.Error(), "ui.invalid") {
		t.Errorf("overridden field: %v", e)
	}
	// Invalid settings are refused before anything is written.
	invalid := before
	invalid.Account.Username = "bad user"
	if _, err := SQLiteEdits(cfg.Provenance(), &before, invalid); asError(t, err).Kind != Validation {
		t.Errorf("invalid form: %v", err)
	}

	// Round trip: stored edits reappear after a "restart" (a new resolution).
	after.Account.Server = before.Account.Server
	for k, v := range edits(t, cfg.Provenance(), &before, after) {
		stored[k] = v
	}
	c.SQLite = SQLiteLayer(stored)
	restarted := ok(t, c)
	if restarted.Config.UI.Theme != ThemeLight || restarted.Config.Account.Port != 5070 ||
		restarted.Source(UITheme) != SQLite || restarted.Source(Server) != Env {
		t.Errorf("after restart: %+v", restarted.Config)
	}
}

func TestFirstRunEditsUseLayerSourcesAndSkipOverrides(t *testing.T) {
	c := ctx()
	c.Env = map[string]string{"GO_MICROSIP_ACCOUNT_SERVER": "env.invalid"}
	if e := fail(t, c); e.Kind != Missing {
		t.Fatalf("first run: %v", e)
	}
	sources, err := LayerSources(c)
	if err != nil || sources[Server] != Env || len(sources) != 1 {
		t.Fatalf("layer sources %v, %v", sources, err)
	}
	c2 := ctx()
	c2.SQLite = SQLiteLayer(map[string]string{"account.username": "1001", "account.domain": "pbx.invalid"})
	form := ok(t, c2).Config
	got := edits(t, sources, nil, form)
	if _, has := got["account.server"]; has {
		t.Error("the environment's value was stored")
	}
	if got["account.username"] != "1001" || got["ui.theme"] != "dark" {
		t.Errorf("edits: %v", got)
	}
	// Without a starting point nothing is ever removed: unset paths in the form must not delete
	// stored ca_file or password_file values.
	for k, v := range got {
		if v == "<nil>" || k == "account.auth_username" || k == "account.ca_file" || k == "account.password_file" {
			t.Errorf("first-run edit %s = %s", k, v)
		}
	}
}

func TestImportPreviewIsValidatedSanitizedAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	text := "schema = 1\n[account]\nusername = \"1001\"\ndomain = \"pbx.invalid\"\nport = 5070\npassword = \"" + secret + "\"\n[ui]\ndnd = true\n"
	path := file(t, dir, "import.toml", text, 0o600)
	files := access{me}
	preview, err := PreviewImport(path, files)
	if err != nil {
		t.Fatal(err)
	}
	values := preview.SQLiteValues()
	if keys := slices.Sorted(maps.Keys(values)); !preview.LiteralPasswordIgnored || values["account.port"] != "5070" ||
		!reflect.DeepEqual(keys, []string{"account.domain", "account.port", "account.username", "ui.dnd"}) {
		t.Errorf("preview: %v, literal ignored %v", values, preview.LiteralPasswordIgnored)
	}
	shown := preview.String()
	if !strings.Contains(shown, `account.domain = "pbx.invalid"`) || !strings.Contains(shown, "--apply") || !strings.Contains(shown, "literal password is not imported") {
		t.Errorf("preview text:\n%s", shown)
	}
	if all := shown + fmt.Sprintf("%+v %#v %v", preview, preview, values); strings.Contains(all, secret) {
		t.Errorf("the password leaked: %s", all)
	}
	if after, _ := os.ReadFile(path); string(after) != text {
		t.Error("the imported file was modified")
	}

	kind := func(name, text string, mode os.FileMode) *Error {
		_, err := PreviewImport(file(t, dir, name, text, mode), files)
		return asError(t, err)
	}
	// A literal password in a group-readable file is refused (CFG-06).
	if e := kind("loose.toml", text, 0o640); e.Kind != InsecureFile || e.Problem != NotPrivate {
		t.Errorf("group-readable import: %v", e)
	}
	// Invalid or incomplete files fail before anything could be written.
	if e := kind("bad.toml", baseTOML+"transport = \"x\"\n", 0o600); e.Kind != Invalid || e.Field != Transport || e.Origin != TOML {
		t.Errorf("invalid import: %v", e)
	}
	if e := kind("partial.toml", "schema = 1\n[ui]\ntheme = \"dark\"\n", 0o600); e.Kind != Missing {
		t.Errorf("partial import: %v", e)
	}
	if e := kind("unknown.toml", "schema = 1\n[account]\nfoo = 1\n", 0o600); e.Kind != UnknownKey {
		t.Errorf("unknown key: %v", e)
	}
	if _, err := PreviewImport(filepath.Join(dir, "absent.toml"), files); asError(t, err).Kind != FileMissing {
		t.Errorf("absent import: %v", err)
	}
}

// The Rust candidate's variables are not read (design D8); the doctor names them so a migrated
// setup that silently lost its settings can be understood. Names only: values may be secrets.
func TestLegacyEnvironmentIsReportedNotRead(t *testing.T) {
	c := ctx()
	c.Env = map[string]string{
		"RUST_MICROSIP_ACCOUNT_USERNAME": "1001", "RUST_MICROSIP_ACCOUNT_DOMAIN": "pbx.invalid", "RUST_MICROSIP_SIP_PASSWORD": secret,
	}
	if e := fail(t, c); e.Kind != Missing {
		t.Fatalf("the legacy variables were read: %v", e)
	}
	report := Doctor(c)
	text := report.String() + report.JSON()
	if report.OK || len(report.IgnoredLegacyEnv) != 3 || !strings.Contains(text, "RUST_MICROSIP_ACCOUNT_DOMAIN") ||
		!strings.Contains(text, "rename them to GO_MICROSIP_*") || strings.Contains(text, secret) {
		t.Errorf("doctor: %s", text)
	}

	c.Env["GO_MICROSIP_ACCOUNT_USERNAME"], c.Env["GO_MICROSIP_ACCOUNT_DOMAIN"] = "1002", "pbx.invalid"
	c.EnvFile = file(t, t.TempDir(), "e.env", "RUST_MICROSIP_UI_THEME=light\n", 0o600)
	cfg := ok(t, c)
	if cfg.Config.Account.Username != "1002" || cfg.PasswordSource != "" || cfg.Config.UI.Theme != ThemeDark || len(cfg.LegacyEnv) != 4 {
		t.Errorf("username %q password %q theme %q legacy %v", cfg.Config.Account.Username, cfg.PasswordSource, cfg.Config.UI.Theme, cfg.LegacyEnv)
	}
	if report := Doctor(c); !report.OK || len(report.IgnoredLegacyEnv) != 4 || !strings.Contains(report.String(), "RUST_MICROSIP_UI_THEME") {
		t.Errorf("doctor: %s", report)
	}
}

func TestFieldsAndEnvironmentNames(t *testing.T) {
	if len(Fields()) != 16 || Server.EnvName() != "GO_MICROSIP_ACCOUNT_SERVER" || DND.EnvName() != "GO_MICROSIP_UI_DND" {
		t.Errorf("fields %d, %s, %s", len(Fields()), Server.EnvName(), DND.EnvName())
	}
	for _, f := range Fields() {
		if back, ok := FieldFromKey(f.Key()); !ok || back != f || f.String() != f.Key() {
			t.Errorf("%d: key %q does not round-trip", f, f.Key())
		}
	}
	if _, ok := FieldFromKey("account.password"); ok {
		t.Error("the password is a field")
	}
}

// A settings form opens before an account exists: the draft has what is there, says what is
// missing, and names the source of each field.
func TestDraftReadsWhatIsThereWithoutAnAccount(t *testing.T) {
	c := Context{SQLite: SQLiteLayer(map[string]string{"ui.theme": "xp", "audio.input_device": "mic-7"}), Env: map[string]string{}}
	draft, sources, err := Draft(c)
	var missing *Error
	if !errors.As(err, &missing) || missing.Kind != Missing {
		t.Fatalf("Draft error = %v, want a missing field", err)
	}
	if draft.UI.Theme != ThemeXP || draft.Audio.InputDevice != "mic-7" || sources[UITheme] != SQLite {
		t.Errorf("draft %+v, theme from %v", draft, sources[UITheme])
	}
	if _, err := Resolve(c); err == nil {
		t.Error("Resolve accepted a configuration without an account")
	}
}
