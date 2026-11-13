package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func valid() AccountSettings {
	return AccountSettings{
		DisplayName: "Lab", Username: "1001", Domain: "pbx.invalid", Server: "192.0.2.1",
		Port: 5061, Transport: TLS, Media: SRTP, Expires: 300,
	}
}

func TestAccountValidation(t *testing.T) {
	long := strings.Repeat("a", 65)
	for name, c := range map[string]struct {
		change func(*AccountSettings)
		want   error
	}{
		"valid":                    {func(*AccountSettings) {}, nil},
		"plain rtp over udp":       {func(a *AccountSettings) { a.Transport, a.Media, a.Port = UDP, PlainRTP, 5060 }, nil},
		"ipv6 server":              {func(a *AccountSettings) { a.Server = "[2001:db8::1]" }, nil},
		"user punctuation":         {func(a *AccountSettings) { a.Username = "a-b_c.d!~*'()&=+$,;?/#" }, nil},
		"auth user set":            {func(a *AccountSettings) { a.AuthUsername = "login" }, nil},
		"empty user":               {func(a *AccountSettings) { a.Username = "" }, ErrEmpty},
		"long user":                {func(a *AccountSettings) { a.Username = long }, ErrTooLong},
		"user with a space":        {func(a *AccountSettings) { a.Username = "10 01" }, ErrInvalidCharacters},
		"user with @":              {func(a *AccountSettings) { a.Username = "1001@x" }, ErrInvalidCharacters},
		"user not ascii":           {func(a *AccountSettings) { a.Username = "joão" }, ErrInvalidCharacters},
		"auth user invalid":        {func(a *AccountSettings) { a.AuthUsername = "a b" }, ErrInvalidCharacters},
		"auth user long":           {func(a *AccountSettings) { a.AuthUsername = long }, ErrTooLong},
		"empty domain":             {func(a *AccountSettings) { a.Domain = "" }, ErrEmpty},
		"domain with a slash":      {func(a *AccountSettings) { a.Domain = "pbx/invalid" }, ErrInvalidCharacters},
		"long domain":              {func(a *AccountSettings) { a.Domain = strings.Repeat("a", 254) }, ErrTooLong},
		"empty server":             {func(a *AccountSettings) { a.Server = "" }, ErrEmpty},
		"server with a space":      {func(a *AccountSettings) { a.Server = "pbx invalid" }, ErrInvalidCharacters},
		"display name with quote":  {func(a *AccountSettings) { a.DisplayName = `La"b` }, ErrInvalidCharacters},
		"display name backslash":   {func(a *AccountSettings) { a.DisplayName = `La\b` }, ErrInvalidCharacters},
		"display name control":     {func(a *AccountSettings) { a.DisplayName = "La\nb" }, ErrInvalidCharacters},
		"display name long":        {func(a *AccountSettings) { a.DisplayName = long }, ErrInvalidCharacters},
		"display name with accent": {func(a *AccountSettings) { a.DisplayName = "João" }, nil},
		"port zero":                {func(a *AccountSettings) { a.Port = 0 }, ErrInvalidPort},
		"srtp over tcp":            {func(a *AccountSettings) { a.Transport = TCP }, ErrSRTPRequiresTLS},
		"srtp over udp":            {func(a *AccountSettings) { a.Transport = UDP }, ErrSRTPRequiresTLS},
	} {
		a := valid()
		c.change(&a)
		if got := a.Validate(); !errors.Is(got, c.want) {
			t.Errorf("%s: Validate = %v, want %v", name, got, c.want)
		}
	}
}

func TestTransportAndMedia(t *testing.T) {
	for _, tr := range []Transport{UDP, TCP, TLS} {
		if got, ok := ParseTransport(tr.String()); !ok || got != tr {
			t.Errorf("ParseTransport(%q) = %v %v", tr.String(), got, ok)
		}
	}
	if UDP.DefaultPort() != 5060 || TCP.DefaultPort() != 5060 || TLS.DefaultPort() != 5061 {
		t.Error("default ports")
	}
	if _, ok := ParseTransport("TLS"); ok {
		t.Error("an upper-case transport was accepted")
	}
	for _, m := range []MediaSecurity{PlainRTP, SRTP} {
		if got, ok := ParseMediaSecurity(m.String()); !ok || got != m {
			t.Errorf("ParseMediaSecurity(%q) = %v %v", m.String(), got, ok)
		}
	}
	if _, ok := ParseMediaSecurity("plainrtp"); ok {
		t.Error("an unknown media label was accepted")
	}
}

func TestDialTarget(t *testing.T) {
	for typed, want := range map[string]string{
		"1002": "1002", " 1002 ": "1002", "*97": "*97", "+5511999990000": "+5511999990000",
		"front-door_1.a": "front-door_1.a", "#31#": "#31#",
	} {
		got, err := ParseDialTarget(typed)
		if err != nil || got.Value() != want {
			t.Errorf("ParseDialTarget(%q) = %q, %v; want %q", typed, got.Value(), err, want)
		}
	}
	for typed, want := range map[string]error{
		"": ErrEmpty, "   ": ErrEmpty, strings.Repeat("1", 65): ErrTooLong, "10 02": ErrInvalidCharacters,
		"1002@pbx": ErrInvalidCharacters, "sip:1002": ErrInvalidCharacters, "10+02": ErrInvalidCharacters,
		"++55": ErrInvalidCharacters, "1002;x=y": ErrInvalidCharacters, "ramal": nil,
	} {
		if _, err := ParseDialTarget(typed); !errors.Is(err, want) {
			t.Errorf("ParseDialTarget(%q) = %v, want %v", typed, err, want)
		}
	}
}

func TestDTMFKey(t *testing.T) {
	events := map[rune]uint8{'0': 0, '5': 5, '9': 9, '*': 10, '#': 11, 'A': 12, 'B': 13, 'C': 14, 'D': 15, 'a': 12, 'd': 15}
	for key, want := range events {
		k, err := NewDTMFKey(key)
		if err != nil || k.Event() != want {
			t.Errorf("NewDTMFKey(%q) = event %d, %v; want %d", key, k.Event(), err, want)
		}
	}
	if k, _ := NewDTMFKey('b'); k.Rune() != 'B' {
		t.Errorf("lower case not folded: %q", k.Rune())
	}
	for _, key := range "E e+ \n１" {
		if _, err := NewDTMFKey(key); !errors.Is(err, ErrInvalidCharacters) {
			t.Errorf("NewDTMFKey(%q) = %v, want ErrInvalidCharacters", key, err)
		}
	}
}

// Every move between phases is either allowed or refused exactly as listed here.
func TestPhaseTransitions(t *testing.T) {
	kinds := []PhaseKind{Dialing, Ringing, Established, Held, Terminating, Ended}
	allowed := map[[2]PhaseKind]bool{
		{Dialing, Ringing}: true, {Dialing, Established}: true, {Dialing, Terminating}: true, {Dialing, Ended}: true,
		{Ringing, Established}: true, {Ringing, Terminating}: true, {Ringing, Ended}: true,
		{Established, Held}: true, {Established, Terminating}: true, {Established, Ended}: true,
		{Held, Established}: true, {Held, Terminating}: true, {Held, Ended}: true,
		{Terminating, Ended}: true,
	}
	for _, from := range kinds {
		for _, to := range kinds {
			got := CallPhase{Kind: from}.CanTransitionTo(CallPhase{Kind: to})
			if got != allowed[[2]PhaseKind{from, to}] {
				t.Errorf("%v -> %v: allowed = %v", from, to, got)
			}
		}
	}
	if !EndedWith(EndBusy).IsFinal() || (CallPhase{Kind: Held}).IsFinal() {
		t.Error("IsFinal")
	}
	if EndedWith(EndBusy).CanTransitionTo(EndedWith(EndNormal)) {
		t.Error("an ended call changed its end reason")
	}
}

func TestStateLabels(t *testing.T) {
	for got, want := range map[string]string{
		RegistrationState{Status: Registered}.String():                                       "registered",
		RegistrationState{Status: RegistrationFailed, Failure: Timeout}.String():             "failed: timeout",
		RegistrationState{Status: RegistrationFailed, Failure: Rejected, Code: 503}.String(): "failed: rejected 503",
		EndedWith(EndNoAnswer).String():                                                      "ended: no_answer",
		CallPhase{Kind: Held}.String():                                                       "held",
		Incoming.String():                                                                    "incoming",
		NoticeOverloaded.String():                                                            "overloaded",
		TransferFailed.String():                                                              "failed",
	} {
		if got != want {
			t.Errorf("label %q, want %q", got, want)
		}
	}
}

// A password, a dialed number and a DTMF key never appear however a command is formatted.
func TestNothingSecretIsPrinted(t *testing.T) {
	target, _ := ParseDialTarget("5511999990000")
	key, _ := NewDTMFKey('7')
	commands := []Command{
		Register{Settings: valid(), Password: NewSecret("s3cret-P4ss")},
		Dial{Target: target},
		Transfer{Call: 3, Target: target},
		SendDTMF{Call: 3, Key: key},
	}
	for _, c := range commands {
		var all []string
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x"} {
			all = append(all, fmt.Sprintf(verb, c))
		}
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, string(raw), fmt.Sprint(c), fmt.Sprintln(&c))
		for _, out := range all {
			for _, secret := range []string{"s3cret", "P4ss", "5511999990000", "7333637265742d50347373", "35353131"} {
				if strings.Contains(out, secret) {
					t.Errorf("%T printed %q in %q", c, secret, out)
				}
			}
		}
	}
	if got := fmt.Sprintf("%v", commands[0]); !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "1001") {
		t.Errorf("Register prints %q: the settings should show, the password as [REDACTED]", got)
	}
	if NewSecret("x").Reveal() != "x" || !NewSecret("").Empty() || target.Value() != "5511999990000" || key.Rune() != '7' {
		t.Error("the accessors must still return the values")
	}
}
