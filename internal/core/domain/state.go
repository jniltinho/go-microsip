package domain

import "strconv"

// RegistrationStatus is the account's registration status as the status bar shows it.
type RegistrationStatus uint8

// Registration statuses.
const (
	// Disabled: no account is configured, or it was unregistered.
	Disabled RegistrationStatus = iota
	// Registering: a REGISTER transaction is in progress.
	Registering
	// Registered: the registrar accepted the binding.
	Registered
	// RegistrationFailed: see RegistrationState.Failure; the engine retries with bounded backoff.
	RegistrationFailed
)

func (s RegistrationStatus) String() string {
	return [...]string{"disabled", "registering", "registered", "failed"}[s]
}

// FailureKind is why registration is not active.
type FailureKind uint8

// Registration failures.
const (
	// NoFailure is the zero value, when the status is not RegistrationFailed.
	NoFailure FailureKind = iota
	// Unauthorized: the registrar refused the credentials.
	Unauthorized
	// Timeout: the registrar or the network did not answer in time.
	Timeout
	// Certificate: TLS certificate or host-name validation failed.
	Certificate
	// Network: name resolution or the connection failed.
	Network
	// Rejected: the registrar answered with another failure status (RegistrationState.Code).
	Rejected
)

func (k FailureKind) String() string {
	return [...]string{"", "unauthorized", "timeout", "certificate", "network", "rejected"}[k]
}

// RegistrationState is the registration status with the reason of a failure.
type RegistrationState struct {
	Status  RegistrationStatus
	Failure FailureKind
	// Code is the SIP status of a Rejected failure.
	Code uint16
}

// String is "registered", "failed: timeout", "failed: rejected 503".
func (r RegistrationState) String() string {
	if r.Status != RegistrationFailed {
		return r.Status.String()
	}
	s := "failed: " + r.Failure.String()
	if r.Failure == Rejected {
		s += " " + strconv.Itoa(int(r.Code))
	}
	return s
}

// CallID identifies one call within an engine session.
type CallID uint64

// Direction is who started the call.
type Direction uint8

// Call directions.
const (
	Outgoing Direction = iota
	Incoming
)

func (d Direction) String() string {
	if d == Incoming {
		return "incoming"
	}
	return "outgoing"
}

// EndReason is why a call ended.
type EndReason uint8

// End reasons.
const (
	// EndNormal: one side hung up an established call.
	EndNormal EndReason = iota
	// EndCanceled: the caller gave up before an answer.
	EndCanceled
	// EndRejected: the callee refused.
	EndRejected
	// EndBusy: the callee was busy.
	EndBusy
	// EndNoAnswer: nobody answered in time.
	EndNoAnswer
	// EndTransferred: the call was handed over by a transfer.
	EndTransferred
	// EndNotAcceptable: no common codec, or the media security did not match.
	EndNotAcceptable
	// EndFailed: a network or protocol failure.
	EndFailed
)

func (e EndReason) String() string {
	return [...]string{"normal", "canceled", "rejected", "busy", "no_answer", "transferred", "not_acceptable", "failed"}[e]
}

// PhaseKind is a call's phase without the reason of its end.
type PhaseKind uint8

// Call phases.
const (
	// Dialing: an outgoing call was sent, nothing came back yet.
	Dialing PhaseKind = iota
	// Ringing: the other side is being alerted (outgoing) or this side is (incoming).
	Ringing
	// Established: both sides talk.
	Established
	// Held: this side put the call on hold.
	Held
	// Terminating: this side is ending the call and waits for the other to confirm.
	Terminating
	// Ended: the call is over; CallPhase.End says why.
	Ended
)

func (k PhaseKind) String() string {
	return [...]string{"dialing", "ringing", "established", "held", "terminating", "ended"}[k]
}

// CallPhase is where a call is in its life.
type CallPhase struct {
	Kind PhaseKind
	// End is the reason, when Kind is Ended.
	End EndReason
}

// EndedWith is the final phase with its reason.
func EndedWith(reason EndReason) CallPhase { return CallPhase{Ended, reason} }

// String is "established", "ended: busy".
func (p CallPhase) String() string {
	if p.Kind == Ended {
		return "ended: " + p.End.String()
	}
	return p.Kind.String()
}

// IsFinal reports whether the call is over.
func (p CallPhase) IsFinal() bool { return p.Kind == Ended }

// CanTransitionTo reports whether the engine may move a call from p to next. Nothing follows the
// end; anything may end; otherwise only the moves below.
func (p CallPhase) CanTransitionTo(next CallPhase) bool {
	if p.Kind == Ended {
		return false
	}
	if next.Kind == Ended {
		return true
	}
	switch p.Kind {
	case Dialing:
		return next.Kind == Ringing || next.Kind == Established || next.Kind == Terminating
	case Ringing:
		return next.Kind == Established || next.Kind == Terminating
	case Established:
		return next.Kind == Held || next.Kind == Terminating
	case Held:
		return next.Kind == Established || next.Kind == Terminating
	}
	return false
}

// TransferKind is the progress of a blind transfer.
type TransferKind uint8

// Transfer progress.
const (
	// NoTransfer: none was asked.
	NoTransfer TransferKind = iota
	// TransferPending: asked, the result is not known yet.
	TransferPending
	// TransferSucceeded: the other side reported success.
	TransferSucceeded
	// TransferFailed: the other side reported the SIP status in TransferState.Code.
	TransferFailed
)

func (k TransferKind) String() string {
	return [...]string{"", "pending", "succeeded", "failed"}[k]
}

// TransferState is the progress of a blind transfer on a call.
type TransferState struct {
	Kind TransferKind
	Code uint16
}

// CallSnapshot is everything a front end knows about a call at one moment. Version grows with
// every change, so a front end can discard a snapshot older than the one it has.
type CallSnapshot struct {
	ID        CallID
	Direction Direction
	// Remote is the other party as the network named it; it is remote-controlled text and is shown
	// only as inert text.
	Remote   string
	Phase    CallPhase
	Muted    bool
	Secure   bool
	Transfer TransferState
	Version  uint64
	// StartedMS, AnsweredMS and EndedMS are when the call began, was first established and ended,
	// in Unix milliseconds; 0 means not yet, and for AnsweredMS in a final snapshot, never. Every
	// snapshot carries them, the last one included: a front end that reads slowly gets intermediate
	// snapshots merged, and must not have to see "established" to know a call was answered.
	StartedMS, AnsweredMS, EndedMS int64
}

// Answered reports whether the call was ever established.
func (c CallSnapshot) Answered() bool { return c.AnsweredMS != 0 }

// Command is what a front end asks of the engine. The types below are all of them.
type Command interface{ command() }

// Commands.
type (
	// Register applies the account settings and registers with the password.
	Register struct {
		Settings AccountSettings
		Password Secret
	}
	// Unregister removes the registration binding.
	Unregister struct{}
	// Dial places an outgoing call.
	Dial struct{ Target DialTarget }
	// Answer answers an incoming call.
	Answer struct{ Call CallID }
	// Reject refuses an incoming call (486 Busy Here).
	Reject struct{ Call CallID }
	// Hangup cancels, refuses or ends a call, whichever its phase needs.
	Hangup struct{ Call CallID }
	// SendDTMF sends one RFC 4733 key.
	SendDTMF struct {
		Call CallID
		Key  DTMFKey
	}
	// Hold puts the call on hold.
	Hold struct{ Call CallID }
	// Resume takes the call off hold.
	Resume struct{ Call CallID }
	// Transfer hands the call over to another target (blind transfer).
	Transfer struct {
		Call   CallID
		Target DialTarget
	}
	// SetMute mutes or unmutes the microphone for the call.
	SetMute struct {
		Call  CallID
		Muted bool
	}
	// SetDND turns do-not-disturb on or off: while on, new incoming calls get 486.
	SetDND struct{ On bool }
	// Shutdown ends every call, unregisters and stops the engine within a bounded time.
	Shutdown struct{}
)

func (Register) command()   {}
func (Unregister) command() {}
func (Dial) command()       {}
func (Answer) command()     {}
func (Reject) command()     {}
func (Hangup) command()     {}
func (SendDTMF) command()   {}
func (Hold) command()       {}
func (Resume) command()     {}
func (Transfer) command()   {}
func (SetMute) command()    {}
func (SetDND) command()     {}
func (Shutdown) command()   {}

// Notice tells a front end that a command was refused or that something failed.
type Notice uint8

// Notices.
const (
	// NoticeBusy: there is already a call.
	NoticeBusy Notice = iota
	// NoticeNotRegistered: the account is not registered.
	NoticeNotRegistered
	// NoticeUnknownCall: the command named a call that does not exist.
	NoticeUnknownCall
	// NoticeInvalidState: the call's phase does not allow the command.
	NoticeInvalidState
	// NoticeAudioUnavailable: the sound devices could not be opened.
	NoticeAudioUnavailable
	// NoticeOverloaded: the command queue is full; the command was not taken.
	NoticeOverloaded
	// NoticeEngineFailed: the engine stopped after an internal failure.
	NoticeEngineFailed
	// NoticeMicrophoneLost: the microphone of the call stopped delivering (unplugged, for example).
	// The call goes on in silence; another microphone is used only when the user chooses one.
	NoticeMicrophoneLost
)

func (n Notice) String() string {
	return [...]string{"busy", "not_registered", "unknown_call", "invalid_state", "audio_unavailable", "overloaded", "engine_failed", "microphone_lost"}[n]
}

// Event is what the engine tells every front end. The types below are all of them.
type Event interface{ event() }

// Events.
type (
	// RegistrationChanged carries the new registration state and the account it is about, as
	// "user@domain": the account the engine uses, whatever a front end has saved or shows. It is
	// empty when the registration is disabled.
	RegistrationChanged struct {
		State   RegistrationState
		Account string
	}
	// CallChanged carries a call's new snapshot; the last one of a call has a final phase.
	CallChanged struct{ Call CallSnapshot }
	// Noticed carries a refusal or a fault.
	Noticed struct{ Notice Notice }
	// Stopped is the last event: the engine stopped after Shutdown.
	Stopped struct{}
)

func (RegistrationChanged) event() {}
func (CallChanged) event()         {}
func (Noticed) event()             {}
func (Stopped) event()             {}
