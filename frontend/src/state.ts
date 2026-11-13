// Presentation state and reducer, equivalent to the Rust candidate's apps/rust-microsip/src/ui/state.rs.
// User intents go through `dispatch`, which sends at most one command; what the window shows about
// registration and calls changes only in `applyEvent`. Times are milliseconds (Date.now()).
import type {
  AccountSettings,
  Approval,
  Backend,
  CallOutcome,
  CallSnapshot,
  Command,
  Contact,
  ContactsCsv,
  EndReason,
  Notice,
  Registration,
  SipTransport,
  Transfer,
  UiEvent,
} from "./api";

/** Maximum characters accepted in the number entry. */
export const MAX_NUMBER_CHARS = 64;
/** How long a notice stays visible. */
export const NOTICE_TTL = 5000;
/** How long an agent request must be on screen before Allow works. */
export const APPROVAL_ARM = 1000;

/** The four destinations of the bottom navigation; "phone" is the keypad and the current call. */
export type Tab = "home" | "calls" | "contacts" | "phone";
export const TABS: readonly { id: Tab; label: string }[] = [
  { id: "home", label: "Home" },
  { id: "calls", label: "Calls" },
  { id: "contacts", label: "Contacts" },
  { id: "phone", label: "Keypad" },
];
/** Which content replaces the tab body. */
export type View = "main" | "settings" | "contactEdit";

/** An action waiting for confirmation: a destructive one, or a call a link asked for. */
export type Confirm =
  | { kind: "deleteCall"; id: number }
  | { kind: "clearHistory" }
  | { kind: "deleteContact"; id: number }
  /** A CSV text of contacts that was read and counted; nothing of it is stored yet. */
  | { kind: "importContacts"; text: string; preview: ContactsCsv }
  | { kind: "quit" }
  | { kind: "dial"; number: string };
const rows = (n: number) => `${n} ${n === 1 ? "row" : "rows"}`;
export const confirmQuestion = (c: Confirm) =>
  c.kind === "dial"
    ? `A link asks to call ${c.number}. Call it?`
    : c.kind === "importContacts"
      ? `Import ${c.preview.contacts} ${c.preview.contacts === 1 ? "contact" : "contacts"} from ${rows(c.preview.records)}?` +
        (c.preview.duplicates ? ` ${rows(c.preview.duplicates)} repeated, skipped.` : "") +
        (c.preview.refused.length
          ? ` ${rows(c.preview.refused.length)} refused, listed below.`
          : "")
      : {
          deleteCall: "Delete this history entry?",
          clearHistory: "Delete the whole call history?",
          deleteContact: "Delete this contact?",
          quit: "Hang up and quit?",
        }[c.kind];
export const confirmButton = (c: Confirm) =>
  c.kind === "quit"
    ? "Hang up and quit"
    : c.kind === "dial"
      ? "Call"
      : c.kind === "importContacts"
        ? "Import"
        : "Delete";

/** The current call plus UI-side timing. */
export interface ActiveCall {
  snapshot: CallSnapshot;
  /** Time of the first "established" snapshot. */
  establishedAt?: number;
}

/** A user intent. */
export type Action =
  | { type: "selectTab"; tab: Tab }
  /** Keypad or keyboard key: DTMF while established, else appended to the number. */
  | { type: "key"; key: string }
  | { type: "backspace" }
  | { type: "clearNumber" }
  | { type: "plus" }
  /** Answer an incoming call, or dial the number. */
  | { type: "call" }
  /** Reject an incoming call, or hang up. */
  | { type: "end" }
  | { type: "toggleMute" }
  | { type: "toggleHold" }
  | { type: "toggleDnd" }
  | { type: "openTransfer" }
  | { type: "confirmTransfer" }
  | { type: "cancelTransfer" }
  /** Put a number into the entry and show the dialpad (no dialing). */
  | { type: "fillNumber"; number: string }
  | { type: "dialNumber"; number: string }
  /** A link asks for a call: nothing is dialed before the confirmation. */
  | { type: "askDial"; number: string }
  | { type: "deleteCall"; id: number }
  | { type: "clearHistory" }
  | { type: "deleteContact"; id: number }
  /** Read a CSV text of contacts and ask before anything is stored. */
  | { type: "importContacts"; text: string }
  | { type: "newContact"; number?: string }
  | { type: "editContact"; contact: Contact }
  | { type: "confirm" }
  | { type: "cancelConfirm" }
  | { type: "openSettings" }
  /** Return to the tabs, discarding unsaved form edits. */
  | { type: "closeView" }
  | { type: "setTheme"; theme: string }
  /** Register the saved account with the password the backend holds; opens settings without one. */
  | { type: "register" }
  /** Validate, persist and register; `password` null keeps the one the backend holds. */
  | { type: "saveAccount"; account: AccountSettings; password: string | null }
  | { type: "unregister" }
  /** Quit; asks for confirmation while a call is active. */
  | { type: "quit" }
  | { type: "quitNow" }
  | { type: "approve"; id: string }
  | { type: "deny"; id: string }
  | { type: "denyAll" };

/** The part of the backend the reducer drives. */
export type Engine = Pick<
  Backend,
  | "send"
  | "saveSettings"
  | "deleteCall"
  | "clearHistory"
  | "deleteContact"
  | "previewContactsCsv"
  | "importContactsCsv"
  | "approve"
  | "deny"
  | "denyAll"
>;

export class UiState {
  tab: Tab = "home";
  view: View = "main";
  /** Number entry buffer. */
  number = "";
  /** Transfer target buffer while the prompt is open. */
  transferTarget: string | null = null;
  registration: Registration = { state: "disabled" };
  call: ActiveCall | null = null;
  confirm: Confirm | null = null;
  /** The contact in the editor while the view is "contactEdit". */
  contactForm: Contact | null = null;
  /** Do-not-disturb as last sent successfully (the engine has no event for it). */
  dnd = false;
  theme = "system";
  account: AccountSettings | null = null;
  /** The backend holds a password for `account` (it is never sent to the page). */
  hasPassword = false;
  /** Shutdown was requested. */
  quitting = false;
  /** The engine reported it stopped. */
  stopped = false;
  /** The engine died; the status bar says so until restart. */
  engineFailed = false;
  /** Pending agent requests, oldest first. */
  approvals: Approval[] = [];
  /** The request on screen and when it appeared (arming delay). */
  approvalShown: { id: string; at: number } | null = null;
  /** Bumped when the stored history changed: lists re-query. */
  historyRevision = 0;
  /** Bumped when the stored contacts changed. */
  contactsRevision = 0;
  private noticeText: { text: string; at: number } | null = null;
  private versions = new Map<number, number>();

  constructor(private engine: Engine) {}

  /** The notice to show at `now`, if still fresh. */
  notice(now: number): string | null {
    return this.noticeText && now - this.noticeText.at < NOTICE_TTL ? this.noticeText.text : null;
  }
  setNotice(text: string, now: number) {
    this.noticeText = { text, at: now };
  }

  get incomingRinging(): boolean {
    const s = this.call?.snapshot;
    return s?.direction === "incoming" && s.phase === "ringing";
  }
  /** Visible label of the call button. */
  get callLabel(): string {
    return this.incomingRinging ? "Answer" : "Call";
  }
  /** Visible label of the end button. */
  get endLabel(): string {
    return this.incomingRinging ? "Reject" : "End";
  }
  /** Why Call is not possible now; null when it is. */
  get cannotCall(): string | null {
    if (this.incomingRinging) return null;
    if (this.call) return "A call is already active";
    return this.number.trim() ? null : "Enter a number first";
  }
  /** Why End is not possible now; null when it is. */
  get cannotEnd(): string | null {
    if (!this.call) return "No active call";
    return this.call.snapshot.phase === "terminating" ? "The call is ending" : null;
  }
  /** The call usable for mute, hold and transfer. */
  get connectedCall(): CallSnapshot | null {
    const s = this.call?.snapshot;
    return s && (s.phase === "established" || s.phase === "held") ? s : null;
  }
  /** Whether keypad keys currently send DTMF. */
  get keypadSendsDtmf(): boolean {
    return this.call?.snapshot.phase === "established";
  }
  /** Connected time at `now`, in milliseconds. */
  duration(now: number): number | null {
    const at = this.call?.establishedAt;
    return at === undefined ? null : Math.max(0, now - at);
  }
  /** Whether Allow works for the request on screen. */
  approvalArmed(now: number): boolean {
    return this.approvalShown !== null && now - this.approvalShown.at >= APPROVAL_ARM;
  }
  /** Status bar account text. */
  get accountText(): string {
    // The engine's own account while it uses one: what is saved may not have been taken yet.
    if (this.registration.state !== "disabled" && this.registration.account)
      return this.registration.account;
    return this.account
      ? `${this.account.username}@${this.account.domain}`
      : "No account configured";
  }

  private async send(command: Command, now: number): Promise<boolean> {
    try {
      await this.engine.send(command);
      return true;
    } catch (refused) {
      this.applyEvent({ type: "notice", notice: refused as Notice }, now);
      return false;
    }
  }
  private pushNumber(c: string) {
    if (this.number.length < MAX_NUMBER_CHARS) this.number += c;
  }
  private closeView() {
    this.view = "main";
    this.contactForm = null;
  }

  /** Apply one user intent, sending at most one command. */
  async dispatch(action: Action, now = Date.now()): Promise<void> {
    switch (action.type) {
      case "selectTab":
        this.closeView();
        this.tab = action.tab;
        return;
      case "key": {
        const key = action.key.toUpperCase();
        const call = this.call?.snapshot;
        if (call?.phase === "established") {
          if (/^[0-9*#A-D]$/.test(key)) await this.send({ type: "dtmf", id: call.id, key }, now);
        } else if (/^[0-9*#]$/.test(key)) this.pushNumber(key);
        return;
      }
      case "backspace":
        this.number = this.number.slice(0, -1);
        return;
      case "clearNumber":
        this.number = "";
        return;
      case "plus":
        this.pushNumber("+");
        return;
      case "call": {
        if (this.incomingRinging && this.call) {
          await this.send({ type: "answer", id: this.call.snapshot.id }, now);
        } else if (this.call) {
          this.setNotice("A call is already active", now);
        } else {
          const target = parseDialTarget(this.number);
          if (target.ok) await this.send({ type: "dial", target: target.value }, now);
          else this.setNotice(validationText(target.error), now);
        }
        return;
      }
      case "end":
        if (this.incomingRinging && this.call)
          await this.send({ type: "reject", id: this.call.snapshot.id }, now);
        else if (this.call && !this.cannotEnd)
          await this.send({ type: "hangup", id: this.call.snapshot.id }, now);
        return;
      case "toggleMute": {
        const call = this.connectedCall;
        if (call) await this.send({ type: "setMute", id: call.id, muted: !call.muted }, now);
        return;
      }
      case "toggleHold": {
        const call = this.connectedCall;
        if (call)
          await this.send({ type: call.phase === "held" ? "resume" : "hold", id: call.id }, now);
        return;
      }
      case "toggleDnd": {
        // No confirmation event exists for DND; the flag follows a successfully queued command.
        const on = !this.dnd;
        if (await this.send({ type: "setDnd", on }, now)) this.dnd = on;
        return;
      }
      case "openTransfer":
        if (this.connectedCall) this.transferTarget = "";
        return;
      case "confirmTransfer": {
        if (this.transferTarget === null) return;
        const call = this.connectedCall;
        if (!call)
          return this.setNotice(
            this.call ? "Available once the call is connected" : "No active call",
            now,
          );
        const target = parseDialTarget(this.transferTarget);
        if (!target.ok) return this.setNotice(validationText(target.error), now);
        if (await this.send({ type: "transfer", id: call.id, target: target.value }, now))
          this.transferTarget = null;
        return;
      }
      case "cancelTransfer":
        this.transferTarget = null;
        return;
      case "fillNumber":
        this.closeView();
        this.number = action.number.slice(0, MAX_NUMBER_CHARS);
        this.tab = "phone";
        return;
      case "dialNumber":
        if (this.call) return this.setNotice("A call is already active", now);
        await this.dispatch({ type: "fillNumber", number: action.number }, now);
        return this.dispatch({ type: "call" }, now);
      case "askDial": {
        // The number is shown as text and dialed only through the same checks as a typed one.
        const asked = parseDialTarget(action.number);
        if (!asked.ok) return this.setNotice("The link is not valid", now);
        // The question is shown over whatever is open: a form being filled in is left as it is.
        this.ask({ kind: "dial", number: asked.value }, now);
        return;
      }
      case "deleteCall":
      case "deleteContact":
        this.ask({ kind: action.type, id: action.id }, now);
        return;
      case "clearHistory":
        this.ask({ kind: "clearHistory" }, now);
        return;
      case "importContacts":
        try {
          const preview = await this.engine.previewContactsCsv(action.text);
          if (preview.contacts === 0)
            return this.setNotice(
              preview.refused.length
                ? `Nothing to import: all ${rows(preview.refused.length)} refused`
                : "Nothing to import: the file has no contacts",
              now,
            );
          this.ask({ kind: "importContacts", text: action.text, preview }, now);
        } catch (e) {
          this.setNotice(`Not imported: ${String(e)}`, now);
        }
        return;
      case "newContact":
        this.closeView();
        this.contactForm = {
          id: 0,
          name: "",
          favorite: false,
          numbers: [{ number: action.number ?? "", label: "" }],
        };
        this.view = "contactEdit";
        return;
      case "editContact":
        this.closeView();
        this.contactForm = JSON.parse(JSON.stringify(action.contact)) as Contact;
        this.view = "contactEdit";
        return;
      case "confirm":
        return this.runConfirm(now);
      case "cancelConfirm":
        this.confirm = null;
        return;
      case "openSettings":
        this.closeView();
        this.view = "settings";
        return;
      case "closeView":
        return this.closeView();
      case "setTheme":
        // Applied at once; a failed save only means it is not remembered.
        this.theme = action.theme;
        await this.engine
          .saveSettings({ theme: action.theme })
          .catch(() => this.setNotice("The skin could not be saved", now));
        return;
      case "register":
        if (!this.account) return this.dispatch({ type: "openSettings" }, now);
        await this.send({ type: "register", account: this.account, password: null }, now);
        return;
      case "saveAccount": {
        const { account, password } = action;
        const error = validateAccount(account);
        if (error) return this.setNotice(validationText(error), now);
        if (password === null && !this.hasPassword)
          return this.setNotice("Password is required", now);
        // The engine keeps its account for the length of a call: nothing is saved or shown as
        // the account meanwhile.
        if (this.call) return this.setNotice(noticeText("busy"), now);
        try {
          await this.engine.saveSettings({ account });
        } catch (e) {
          return this.setNotice(String(e), now);
        }
        this.account = account;
        if (await this.send({ type: "register", account, password }, now)) {
          this.hasPassword = true;
          this.closeView();
        }
        return;
      }
      case "unregister":
        await this.send({ type: "unregister" }, now);
        return;
      case "quit":
        if (this.call && !this.quitting) this.ask({ kind: "quit" }, now);
        else await this.quitIfIdle(now);
        return;
      case "quitNow":
        return this.quitNow(now);
      case "approve":
        if (this.approvalShown?.id === action.id && this.approvalArmed(now))
          await this.decide("approve", action.id, now);
        return;
      case "deny":
        return this.decide("deny", action.id, now);
      case "denyAll":
        return this.decide("denyAll", "", now);
    }
  }

  private async decide(what: "approve" | "deny" | "denyAll", id: string, now: number) {
    await this.engine[what](id).catch((e: unknown) => this.setNotice(String(e), now));
  }

  /**
   * Puts a question on the screen, unless one is there. The question that was read is the one a
   * yes answers, whatever arrives meanwhile: a link, the close button, a late answer of the
   * engine. What is not asked is said in a notice.
   */
  private ask(question: Confirm, now: number) {
    if (!this.confirm) this.confirm = question;
    else if (JSON.stringify(this.confirm) !== JSON.stringify(question))
      this.setNotice("Answer the question on the screen first", now);
  }

  /**
   * Quit without a question, as far as this page knows of no call. The engine decides: it stops
   * only if it has none, and a call this page has not heard of yet comes back as "busy", which is
   * when the question is asked after all.
   */
  private async quitIfIdle(now: number) {
    if (this.quitting) return;
    this.quitting = true;
    try {
      await this.engine.send({ type: "quit" });
    } catch (refused) {
      this.quitting = false;
      // The engine's answer may come after another question was put on the screen.
      if (refused === "busy") this.ask({ kind: "quit" }, now);
      else this.applyEvent({ type: "notice", notice: refused as Notice }, now);
    }
  }
  /** Quit that was confirmed, or that a termination signal asks for: every call ends. */
  private async quitNow(now: number) {
    if (this.quitting) return;
    this.quitting = true;
    this.confirm = null;
    // Refused (the engine was busy): the notice says so, and Quit can be tried again.
    if (!(await this.send({ type: "shutdown" }, now))) this.quitting = false;
  }

  private async runConfirm(now: number) {
    const confirm = this.confirm;
    this.confirm = null;
    if (!confirm) return;
    try {
      switch (confirm.kind) {
        case "quit":
          return await this.quitNow(now);
        case "dial":
          // The yes is when the window leaves what was open, for the call.
          if (!this.call) this.closeView();
          return await this.dispatch({ type: "dialNumber", number: confirm.number }, now);
        case "deleteCall":
          await this.engine.deleteCall(confirm.id);
          break;
        case "clearHistory":
          await this.engine.clearHistory();
          break;
        case "deleteContact":
          await this.engine.deleteContact(confirm.id);
          this.contactsRevision++;
          return;
        case "importContacts": {
          const done = await this.engine.importContactsCsv(confirm.text);
          this.contactsRevision++;
          return this.setNotice(
            `Imported: ${done.created} new, ${done.merged} updated, ${done.numbers} numbers added`,
            now,
          );
        }
      }
      this.historyRevision++;
    } catch (e) {
      this.setNotice(String(e), now);
    }
  }

  /** Apply one backend event. */
  applyEvent(event: UiEvent, now = Date.now()) {
    switch (event.type) {
      case "registration":
        this.registration = event.registration;
        return;
      case "notice":
        // While quitting or after a normal stop the queue closes on purpose.
        if (event.notice === "engine_failed" && (this.stopped || this.quitting)) return;
        this.engineFailed ||= event.notice === "engine_failed";
        this.setNotice(noticeText(event.notice), now);
        return;
      case "stopped":
        this.stopped = true;
        return;
      case "approvals":
        this.approvals = event.approvals;
        this.approvalSeen(now);
        return;
      case "dnd":
        this.dnd = event.dnd;
        return;
      case "call":
        this.applyCall(event.call, now);
        this.approvalSeen(now); // a ringing call hides a request, and shows it again when it goes
        return;
    }
  }

  /**
   * Notes when the first request came on screen. Behind the dialog of a ringing call it is not on
   * screen: Allow arms a second after the person can see what is asked, not after it arrived.
   */
  private approvalSeen(now: number) {
    const first = this.incomingRinging ? undefined : this.approvals[0];
    if (!first) this.approvalShown = null;
    else if (this.approvalShown?.id !== first.id) this.approvalShown = { id: first.id, at: now };
  }

  private applyCall(snapshot: CallSnapshot, now: number) {
    if ((this.versions.get(snapshot.id) ?? -1) >= snapshot.version) return;
    this.versions.set(snapshot.id, snapshot.version);
    const current = this.call?.snapshot.id === snapshot.id;
    if (snapshot.phase === "ended") {
      if (current) {
        this.call = null;
        this.transferTarget = null;
      }
      // The backend recorded the call; the list shows it on the next query.
      this.historyRevision++;
      return;
    }
    if (snapshot.transfer?.state === "succeeded" && current) this.transferTarget = null;
    if (this.call && current) {
      if (snapshot.phase === "established") this.call.establishedAt ??= now;
      this.call.snapshot = snapshot;
    } else if (!this.call) {
      // One call at a time: a second call is shown only once it ends (in the history).
      this.call = { snapshot, establishedAt: snapshot.phase === "established" ? now : undefined };
    }
  }
}

/** Why a settings field or dial string is not acceptable (B1.15). */
export type ValidationError =
  | "empty"
  | "tooLong"
  | "invalidCharacters"
  | "invalidPort"
  | "srtpRequiresTls";
type Parsed<T> = { ok: true; value: T } | { ok: false; error: ValidationError };

function bounded(value: string, max: number, allowed: RegExp): ValidationError | null {
  if (!value) return "empty";
  if (value.length > max) return "tooLong";
  return allowed.test(value) ? null : "invalidCharacters";
}

/** A dial string: 1..64 of ASCII alphanumerics and `*#+-_.`, `+` only first; trimmed. */
export function parseDialTarget(text: string): Parsed<string> {
  const value = text.trim();
  const error =
    bounded(value, 64, /^[A-Za-z0-9*#+\-_.]+$/) ??
    (value.slice(1).includes("+") ? "invalidCharacters" : null);
  return error ? { ok: false, error } : { ok: true, value };
}

/** Default registrar port of a transport. */
export const defaultPort = (transport: SipTransport) => (transport === "tls" ? 5061 : 5060);

/** The account rules of the core (`AccountSettings::validate`); the backend checks them again. */
export function validateAccount(a: AccountSettings): ValidationError | null {
  const user = /^[A-Za-z0-9\-_.!~*'()&=+$,;?/#]+$/;
  const host = /^[A-Za-z0-9\-.:[\]]+$/;
  return (
    bounded(a.username, 64, user) ??
    (a.authUsername ? bounded(a.authUsername, 64, user) : null) ??
    bounded(a.domain, 253, host) ??
    bounded(a.server, 253, host) ??
    // eslint-disable-next-line no-control-regex
    (a.displayName.length > 64 || /[\u0000-\u001f\u007f"\\]/.test(a.displayName)
      ? "invalidCharacters"
      : null) ??
    (Number.isInteger(a.port) && a.port >= 1 && a.port <= 65535 ? null : "invalidPort") ??
    (a.media === "srtp" && a.transport !== "tls" ? "srtpRequiresTls" : null)
  );
}

/** Human text for a validation error; never echoes input. */
export const validationText = (error: ValidationError) =>
  ({
    empty: "A required field is empty",
    tooLong: "A field is too long",
    invalidCharacters: "A field contains unsupported characters",
    invalidPort: "Port must be between 1 and 65535",
    srtpRequiresTls: "SRTP requires TLS signaling",
  })[error];

/** Status bar registration text. */
export function registrationText(r: Registration): string {
  switch (r.state) {
    case "disabled":
      return "Disabled";
    case "registering":
      return "Registering\u2026";
    case "registered":
      return "Registered";
    case "failed":
      return `Failed: ${
        {
          unauthorized: "wrong credentials",
          timeout: "no response",
          certificate: "certificate rejected",
          network: "network error",
          rejected: `rejected (${r.code ?? "?"})`,
        }[r.reason ?? "network"]
      }`;
  }
}

/** Human phase text for a call. */
export function phaseText(call: CallSnapshot): string {
  switch (call.phase) {
    case "dialing":
      return "Calling\u2026";
    case "ringing":
      return call.direction === "outgoing" ? "Ringing\u2026" : "Incoming call";
    case "established":
      return "Connected";
    case "held":
      return "On hold";
    case "terminating":
      return "Ending\u2026";
    case "ended":
      return endReasonText(call.endReason ?? "failed");
  }
}

/** Human text for a call outcome. */
export const endReasonText = (reason: EndReason) =>
  ({
    normal: "Ended",
    canceled: "Canceled",
    rejected: "Rejected",
    busy: "Busy",
    no_answer: "No answer",
    transferred: "Transferred",
    not_acceptable: "Not acceptable",
    failed: "Failed",
    answered_elsewhere: "Answered on another phone",
  })[reason];

/** Human text for a transfer state. */
export const transferText = (t: Transfer) =>
  t.state === "pending"
    ? "Transfer pending\u2026"
    : t.state === "succeeded"
      ? "Transfer succeeded"
      : `Transfer failed (${t.code ?? "?"})`;

/** Human text for an engine notice. */
export const noticeText = (notice: Notice) =>
  ({
    busy: "Another call is already active",
    not_registered: "Not registered",
    unknown_call: "The call no longer exists",
    invalid_state: "Not possible in the current call state",
    audio_unavailable: "No audio device; the call continues without local audio",
    overloaded: "Busy; the action was dropped, try again",
    engine_failed: "The phone engine stopped; restart the application",
    microphone_lost: "The microphone stopped; the call continues without it",
    microphone_denied:
      "Windows denies the microphone: allow it in Settings > Privacy & security > Microphone; the call continues without local audio",
  })[notice] ?? "The phone engine stopped; restart the application";

/** Visible label of a history outcome. */
export const outcomeText = (outcome: CallOutcome) =>
  ({
    answered: "Answered",
    missed: "Missed",
    canceled: "Canceled",
    rejected: "Rejected",
    busy: "Busy",
    no_answer: "No answer",
    failed: "Failed",
    interrupted: "Interrupted",
  })[outcome];

/** Coarse age of a history entry from elapsed seconds. */
export function ageText(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86_400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86_400)} d ago`;
}

/** `m:ss` or `h:mm:ss` from milliseconds. */
export function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  const two = (n: number) => String(n).padStart(2, "0");
  return s >= 3600
    ? `${Math.floor(s / 3600)}:${two(Math.floor(s / 60) % 60)}:${two(s % 60)}`
    : `${Math.floor(s / 60)}:${two(s % 60)}`;
}
