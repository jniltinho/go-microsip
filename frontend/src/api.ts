// The backend contract, and the only module that reaches the backend. Screens and the reducer import
// from here; nothing else imports the Wails runtime or the mock.
//
// Two transports, chosen at build time by the Vite mode (vite.config.ts defines VITE_BACKEND):
//   - wails (`vp build`): methods bound on internal/gui.App, events on EVENT_NAME;
//   - mock (`vp dev`, `vp build --mode mock`): a simulated engine with fictional data (mock.ts).
//
// The types mirror docs/migration/parity-matrix.md B1 (commands) and B2 (events).

/** SIP signaling transport. Default ports: udp/tcp 5060, tls 5061. */
export type SipTransport = "udp" | "tcp" | "tls";
/** Media protection; "srtp" is valid only with "tls". */
export type MediaSecurity = "rtp" | "srtp";

/** The single SIP account (B1.14). It never carries the password. */
export interface AccountSettings {
  displayName: string;
  username: string;
  /** Empty: authenticate with `username`. */
  authUsername: string;
  domain: string;
  server: string;
  port: number;
  transport: SipTransport;
  media: MediaSecurity;
  /** Registration lifetime in seconds. */
  expires: number;
}

/** Why registration failed (B2.1); `code` accompanies "rejected". */
export type RegistrationFailure =
  | "unauthorized"
  | "timeout"
  | "certificate"
  | "network"
  | "rejected";
/** Registration state (B2.1). */
export interface Registration {
  state: "disabled" | "registering" | "registered" | "failed";
  reason?: RegistrationFailure;
  code?: number;
  /**
   * The account the engine uses, as "user@domain"; absent when it uses none. The window shows this
   * one: a saved account the engine has not taken (a call was in the way) is not the live one.
   */
  account?: string;
}

export type Direction = "outgoing" | "incoming";
/** Call lifecycle phase (B2.5); "ended" is final and carries `endReason`. */
export type CallPhase = "dialing" | "ringing" | "established" | "held" | "terminating" | "ended";
/** Why a call ended (B2.6). */
export type EndReason =
  | "normal"
  | "canceled"
  | "rejected"
  | "busy"
  | "no_answer"
  | "transferred"
  | "not_acceptable"
  | "failed"
  | "answered_elsewhere";
/** Progress of a blind transfer (B2.7); `code` accompanies "failed". */
export interface Transfer {
  state: "pending" | "succeeded" | "failed";
  code?: number;
}

/** Immutable view of one call (B2.2). */
export interface CallSnapshot {
  id: number;
  direction: Direction;
  /** Remote party user part or display text. Remote-controlled: render as text only. */
  remote: string;
  phase: CallPhase;
  /** Present only when `phase` is "ended". */
  endReason?: EndReason;
  muted: boolean;
  /** Media is SRTP-protected. */
  secure: boolean;
  transfer?: Transfer;
  /** Increases with every change; older or equal versions are ignored. */
  version: number;
}

/** A refused command or a fault (B2.3). */
export type Notice =
  | "busy"
  | "not_registered"
  | "unknown_call"
  | "invalid_state"
  | "audio_unavailable"
  | "overloaded"
  | "engine_failed"
  | "microphone_lost"
  | "microphone_denied";
export const NOTICES: readonly Notice[] = [
  "busy",
  "not_registered",
  "unknown_call",
  "invalid_state",
  "audio_unavailable",
  "overloaded",
  "engine_failed",
  "microphone_lost",
  "microphone_denied",
];

/** An agent (CLI/MCP) request waiting for the person at the window. */
export interface Approval {
  /** Operation id. */
  id: string;
  /** Who asks. Remote-controlled text. */
  requester: string;
  /** What would happen. Remote-controlled text. */
  description: string;
  /** Decision deadline, Unix milliseconds. */
  deadline: number;
}

/** The permissions a grant may carry, with what each lets a program do. */
export const GRANT_SCOPES = [
  { id: "read:contacts", label: "Read contacts" },
  { id: "read:history", label: "Read call history" },
  { id: "read:devices", label: "Read audio devices" },
  { id: "read:config", label: "Read configuration" },
  { id: "control:dial", label: "Call (May call)" },
  { id: "control:answer", label: "Answer (May answer)" },
  { id: "control:reject", label: "Reject (May answer)" },
  { id: "control:hangup", label: "Hang up" },
  { id: "control:dtmf", label: "Send DTMF" },
  { id: "control:mute", label: "Mute" },
  { id: "control:hold", label: "Hold" },
  { id: "control:transfer", label: "Transfer (May call)" },
  { id: "control:dnd", label: "Do not disturb" },
] as const;

/** What the person grants to a program (CLI/MCP) in Settings, Agent access. */
export interface GrantSpec {
  scopes: string[];
  /** Exact numbers the program may call or transfer to. */
  destinations: string[];
  /** Exact callers the program may answer or reject. */
  incoming: string[];
  /** Validity, 1 to 60 minutes. */
  minutes: number;
  /** Calls (dial or answer) allowed in all, at most 100. */
  quota: number;
}

/** A grant that can still be used. The secret is in its file and never comes here. */
export interface Grant {
  id: string;
  scopes: string[];
  destinations: string[];
  incoming: string[];
  /** Unix milliseconds. */
  expires: number;
  quota: number;
  used: number;
  /** Exported for the headless service, and not used yet. */
  bootstrap: boolean;
}

/** One line of the record of agent requests and decisions. Never a number or digits. */
export interface AuditEntry {
  /** Unix milliseconds. */
  at: number;
  operation: string;
  decision: string;
  detail: string;
}

/** Everything the backend pushes, in order. Final call snapshots are never dropped. */
export type UiEvent =
  | { type: "registration"; registration: Registration }
  | { type: "call"; call: CallSnapshot }
  | { type: "notice"; notice: Notice }
  /** The engine stopped after "shutdown" (B2.4). */
  | { type: "stopped" }
  /** The complete list of pending agent requests, oldest first. */
  | { type: "approvals"; approvals: Approval[] }
  /** Do-not-disturb was changed by someone else than this page: an agent, with consent. */
  | { type: "dnd"; dnd: boolean };

/**
 * What a subscriber receives: the engine's events, the desktop's light or dark preference, and
 * "quit" when the window's close button (without a tray) or the tray's Quit asks the page to leave,
 * and "link" when a sip: or tel: link asks for a call.
 */
export type BackendEvent =
  | UiEvent
  | { type: "desktop"; dark: boolean }
  | { type: "quit" }
  /** A sip: or tel: link was handed to the application: the page asks before calling `number`. */
  | { type: "link"; number: string };

/** A request to the engine (B1.1-B1.13). */
export type Command =
  /** Persisted password is used when `password` is null. */
  | { type: "register"; account: AccountSettings; password: string | null }
  | { type: "unregister" }
  /** `target`: 1..64 of alphanumerics and `*#+-_.`, `+` only first. */
  | { type: "dial"; target: string }
  | { type: "answer"; id: number }
  | { type: "reject"; id: number }
  | { type: "hangup"; id: number }
  /** `key`: one of `0-9 * # A-D`. */
  | { type: "dtmf"; id: number; key: string }
  | { type: "hold"; id: number }
  | { type: "resume"; id: number }
  | { type: "transfer"; id: number; target: string }
  | { type: "setMute"; id: number; muted: boolean }
  | { type: "setDnd"; on: boolean }
  /**
   * Quit nobody was asked about: the engine stops only if it has no call, and refuses with "busy"
   * otherwise. Whether there is a call is the engine's to say: the page hears of calls later.
   */
  | { type: "quit" }
  /** Quit a person said yes to: every call ends, the account unregisters, the engine stops. */
  | { type: "shutdown" };

/** Saved, secret-free settings. */
export interface Settings {
  account: AccountSettings | null;
  /** A password for `account` is held (session or keyring); it is never returned. */
  hasPassword: boolean;
  /** Skin id (`ui.theme`); unknown values resolve to "system". */
  theme: string;
  /** Capture device id; empty is the system default. */
  audioInput: string;
  /** Playback device id; empty is the system default. */
  audioOutput: string;
  dnd: boolean;
}
/** The persisted part of the settings the window may change. */
export type SettingsPatch = Partial<
  Pick<Settings, "account" | "theme" | "audioInput" | "audioOutput">
>;

export interface AudioDevice {
  id: string;
  name: string;
  isDefault: boolean;
}
export interface AudioDevices {
  inputs: AudioDevice[];
  outputs: AudioDevice[];
}

/** The settings' sound test: a tone on the chosen speaker, the level of the chosen microphone. */
export interface AudioTest {
  running: boolean;
  /** The microphone's peak, from 0 (silence) to 1 (full scale). */
  level: number;
}

/** What importing a CSV text of contacts would do; nothing is stored by asking. */
export interface ContactsCsv {
  /** Contacts the text holds, new or to be given numbers. */
  contacts: number;
  /** Its data rows, and those that repeat another and are skipped. */
  records: number;
  duplicates: number;
  refused: { line: number; reason: string }[];
}
export interface ContactsImported {
  created: number;
  merged: number;
  numbers: number;
}

export interface ContactNumber {
  number: string;
  /** Optional label such as "work"; empty when none. */
  label: string;
}
export interface Contact {
  /** Row id; 0 in `saveContact` adds a new contact. */
  id: number;
  name: string;
  favorite: boolean;
  numbers: ContactNumber[];
}

export type HistoryFilter = "all" | "incoming" | "outgoing" | "missed";
export type CallOutcome =
  | "answered"
  | "missed"
  | "canceled"
  | "rejected"
  | "busy"
  | "no_answer"
  | "failed"
  | "interrupted";
export interface HistoryEntry {
  id: number;
  direction: Direction;
  remote: string;
  outcome: CallOutcome;
  /** Start time, Unix seconds UTC. */
  startedUtc: number;
  /** Established time in milliseconds; absent when never answered. */
  durationMs?: number;
}

/** A state the mock can be put in from the menu; the Wails transport has none. */
export interface Scenario {
  id: string;
  label: string;
}

/**
 * What a transport implements. Queries reject with a message (string). `send` resolves once the
 * command is queued and rejects with a `Notice` when the engine refuses it.
 */
export interface Backend {
  /** The application's version and the commit it was built from. */
  version(): Promise<string>;
  /** Tells the backend the page has drawn; `--self-test` then exits 0. */
  ready(): Promise<void>;
  /** Starts event delivery; the current registration, call and approvals are replayed first. */
  subscribe(handler: (event: BackendEvent) => void): Promise<() => void>;
  send(command: Command): Promise<void>;
  settings(): Promise<Settings>;
  saveSettings(patch: SettingsPatch): Promise<void>;
  audioDevices(): Promise<AudioDevices>;
  /** Starts the sound test; it ends by itself, on a call, or on stopAudioTest. Refused ("busy") during a call. */
  startAudioTest(): Promise<void>;
  stopAudioTest(): Promise<void>;
  audioTest(): Promise<AudioTest>;
  /** Contacts whose name or number contains `search`, favorites first. */
  contacts(search: string): Promise<Contact[]>;
  saveContact(contact: Contact): Promise<Contact>;
  deleteContact(id: number): Promise<void>;
  /** Reads a CSV text (name,number[,label]) and says what an import would do. Stores nothing. */
  previewContactsCsv(text: string): Promise<ContactsCsv>;
  /** Stores the contacts of the text, without its refused rows. */
  importContactsCsv(text: string): Promise<ContactsImported>;
  /** History entries, newest first, whose remote party contains `search`. */
  history(filter: HistoryFilter, search: string): Promise<HistoryEntry[]>;
  deleteCall(id: number): Promise<void>;
  clearHistory(): Promise<void>;
  approve(id: string): Promise<void>;
  deny(id: string): Promise<void>;
  /** Denies every pending request and pauses new ones. */
  denyAll(): Promise<void>;
  /** The grants that can still be used, newest first. */
  grants(): Promise<Grant[]>;
  /**
   * Issues a grant and writes its secret to a new private file; resolves with the file's path.
   * `bootstrap` exports the single-use grant that starts the headless service.
   */
  createGrant(spec: GrantSpec, bootstrap: boolean): Promise<string>;
  revokeGrant(id: string): Promise<void>;
  /** The latest lines of the record of agent requests, newest first. */
  auditLog(): Promise<AuditEntry[]>;
  clearAudit(): Promise<void>;
  scenarios(): Promise<Scenario[]>;
  simulate(id: string): Promise<void>;
}

/** Wails event carrying one `UiEvent` as its data. */
export const EVENT_NAME = "go-microsip:event";
const SERVICE = "github.com/jniltinho/go-microsip/internal/gui.App.";

type Wails = typeof import("@wailsio/runtime");

function wails({ Call, Events }: Wails): Backend {
  // A failed call rejects with the Go error's text alone.
  const call = <T>(method: string, ...args: unknown[]) =>
    (Call.ByName(SERVICE + method, ...args) as Promise<T>).catch((e: unknown) => {
      throw e instanceof Error ? e.message : e;
    });
  const args = (c: Command): [string, ...unknown[]] => {
    switch (c.type) {
      case "register":
        return ["Register", c.account, c.password];
      case "unregister":
        return ["Unregister"];
      case "dial":
        return ["Dial", c.target];
      case "answer":
        return ["Answer", c.id];
      case "reject":
        return ["Reject", c.id];
      case "hangup":
        return ["Hangup", c.id];
      case "dtmf":
        return ["Dtmf", c.id, c.key];
      case "hold":
        return ["Hold", c.id];
      case "resume":
        return ["Resume", c.id];
      case "transfer":
        return ["Transfer", c.id, c.target];
      case "setMute":
        return ["SetMute", c.id, c.muted];
      case "setDnd":
        return ["SetDnd", c.on];
      case "quit":
        return ["Quit"];
      case "shutdown":
        return ["Shutdown"];
    }
  };
  // `--self-test` asks what its probe left in the page; a page that was navigated away is not
  // here to answer. The probe's own questions are answered here, by a script of the page and in a
  // task of its own: what the window injects, this event's delivery included, runs with more
  // rights than the page has, so only a later task says what the content security policy and the
  // webview allow the page.
  Events.On("go-microsip:probe", () => {
    setTimeout(() => {
      let report = document.documentElement.dataset.probe ?? "";
      if (report.includes('"inline"')) {
        const probe = JSON.parse(report) as Record<string, unknown>;
        try {
          // The one eval of the page: it must throw, and the self-test fails when it does not.
          // eslint-disable-next-line no-eval
          probe.eval = globalThis.eval("1") === 1;
        } catch {
          probe.eval = false;
        }
        // Only where the window guards its webview: without a guard a new window really opens.
        if (probe.guarded) {
          try {
            const opened = window.open("https://example.invalid/new-window");
            probe.opened = opened !== null;
            opened?.close();
          } catch {
            probe.opened = false;
          }
        }
        report = JSON.stringify(probe);
      }
      void call("Probed", report);
    }, 0);
  });
  return {
    version: () => call("Version"),
    ready: () => call("Ready"),
    subscribe: async (handler) => {
      const off = Events.On(EVENT_NAME, (e: { data: BackendEvent }) => {
        // The page is there and runs its own Quit: the window need not ask by itself.
        if (e.data.type === "quit") void call("QuitAsked");
        handler(e.data);
      });
      await call("Subscribe");
      return off;
    },
    // A refusal is the notice's name; anything else means the engine is not there.
    send: (c) =>
      call<void>(...args(c)).catch((e: unknown) => {
        throw NOTICES.includes(e as Notice) ? e : "engine_failed";
      }),
    settings: () => call("Settings"),
    saveSettings: (patch) => call("SaveSettings", patch),
    audioDevices: () => call("AudioDevices"),
    startAudioTest: () => call("StartAudioTest"),
    stopAudioTest: () => call("StopAudioTest"),
    audioTest: () => call("AudioTest"),
    contacts: (search) => call("Contacts", search),
    saveContact: (contact) => call("SaveContact", contact),
    deleteContact: (id) => call("DeleteContact", id),
    previewContactsCsv: (text) => call("PreviewContactsCSV", text),
    importContactsCsv: (text) => call("ImportContactsCSV", text),
    history: (filter, search) => call("History", filter, search),
    deleteCall: (id) => call("DeleteCall", id),
    clearHistory: () => call("ClearHistory"),
    approve: (id) => call("Approve", id),
    deny: (id) => call("Deny", id),
    denyAll: () => call("DenyAll"),
    grants: () => call("Grants"),
    createGrant: (spec, bootstrap) => call("CreateGrant", spec, bootstrap),
    revokeGrant: (id) => call("RevokeGrant", id),
    auditLog: () => call("AuditLog"),
    clearAudit: () => call("ClearAudit"),
    scenarios: () => Promise.resolve([]),
    simulate: () => Promise.reject("Not available"),
  };
}

/** True in the mock build: the window says so and offers the scenarios. */
export const isMock = import.meta.env.VITE_BACKEND === "mock";

// Loaded on first use, so importing this module has no side effect.
let loaded: Promise<Backend> | undefined;
const backend = () =>
  (loaded ??= isMock
    ? import("./mock").then((m) => m.createMock())
    : import("@wailsio/runtime").then(wails));

const via =
  <K extends keyof Backend>(method: K) =>
  (...args: Parameters<Backend[K]>): ReturnType<Backend[K]> =>
    backend().then((b) => (b[method] as (...a: unknown[]) => unknown)(...args)) as ReturnType<
      Backend[K]
    >;

export const version = via("version");
export const ready = via("ready");
export const subscribe = via("subscribe");
export const send = via("send");
export const settings = via("settings");
export const saveSettings = via("saveSettings");
export const audioDevices = via("audioDevices");
export const startAudioTest = via("startAudioTest");
export const stopAudioTest = via("stopAudioTest");
export const audioTest = via("audioTest");
export const contacts = via("contacts");
export const saveContact = via("saveContact");
export const deleteContact = via("deleteContact");
export const previewContactsCsv = via("previewContactsCsv");
export const importContactsCsv = via("importContactsCsv");
export const history = via("history");
export const deleteCall = via("deleteCall");
export const clearHistory = via("clearHistory");
export const approve = via("approve");
export const deny = via("deny");
export const denyAll = via("denyAll");
export const grants = via("grants");
export const createGrant = via("createGrant");
export const revokeGrant = via("revokeGrant");
export const auditLog = via("auditLog");
export const clearAudit = via("clearAudit");
export const scenarios = via("scenarios");
export const simulate = via("simulate");
