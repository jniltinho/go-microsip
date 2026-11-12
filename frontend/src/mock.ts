// A simulated engine for `vp dev` and the screenshot runs: fictional data, no network, no audio.
// Imported only by api.ts and only in the mock build. Numbers with a scripted outcome:
//   1007 not acceptable, 1008 no answer, 1009 busy (also as a transfer target: fails with 486).
// Registration: password "wrong" is refused, server "down.invalid" times out.
// `?scene=<scenario id>` puts the engine in that state before the first event.
import { GRANT_SCOPES } from "./api";
import type {
  AccountSettings,
  Approval,
  AuditEntry,
  Backend,
  CallOutcome,
  CallSnapshot,
  Command,
  Contact,
  EndReason,
  Grant,
  HistoryEntry,
  Notice,
  Registration,
  Scenario,
  Settings,
  UiEvent,
} from "./api";

const THEME_KEY = "go-microsip.mock.theme";
const SCENARIOS: Scenario[] = [
  { id: "incoming", label: "Incoming call" },
  { id: "hostile", label: "Incoming call, hostile name" },
  { id: "active", label: "Active call" },
  { id: "held", label: "Call on hold" },
  { id: "muted", label: "Muted call" },
  { id: "transfer-failed", label: "Failed transfer" },
  { id: "approval", label: "Agent request" },
  { id: "register-failed", label: "Registration failure" },
  { id: "audio", label: "No audio device" },
  { id: "engine-failed", label: "Engine failure" },
];
/** `?scene=no-engine`: every call fails, as in a build whose backend has no engine yet. */
const NO_ENGINE = "The phone engine is not part of this build";

const now = () => Math.floor(Date.now() / 1000);
const copy = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;
// Storage may be blocked (private window); the mock then forgets the skin.
const stored = (key: string) => {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
};

export function createMock(): Backend {
  let handler: (event: UiEvent) => void = () => {};
  let registration: Registration = { state: "registered" };
  let call: CallSnapshot | undefined;
  let answeredAt: number | undefined;
  let startedUtc = 0;
  let approvals: Approval[] = [];
  // One grant and a short record, so that Agent access has something to list.
  let grants: Grant[] = [
    {
      id: "grant-7-0123456789abcdef01234567",
      scopes: ["read:history", "control:dial"],
      destinations: ["1003"],
      incoming: [],
      expires: Date.now() + 12 * 60_000,
      quota: 2,
      used: 1,
      bootstrap: false,
    },
  ];
  let audit: AuditEntry[] = [
    { at: Date.now() - 40_000, operation: "call_dial", decision: "succeeded", detail: "-" },
    {
      at: Date.now() - 41_000,
      operation: "call_dial",
      decision: "granted",
      detail: "destination ***03 by grant grant-7-0123456789abcdef01234567",
    },
    {
      at: Date.now() - 180_000,
      operation: "grant",
      decision: "issued",
      detail: "2 scopes, 15 min",
    },
  ];
  let failed = false;
  let noAudio = false;
  let nextCall = 1;
  let nextRow = 100;
  let timers: ReturnType<typeof setTimeout>[] = [];
  let account: AccountSettings | null = {
    displayName: "Lab",
    username: "1001",
    authUsername: "",
    domain: "pbx.invalid",
    server: "192.0.2.1",
    port: 5061,
    transport: "tls",
    media: "srtp",
    expires: 300,
  };
  const saved = {
    hasPassword: true,
    theme: stored(THEME_KEY) ?? "system",
    audioInput: "",
    audioOutput: "",
    dnd: false,
  };
  let people: Contact[] = [
    { id: 1, name: "Ana Lima", favorite: true, numbers: [{ number: "1002", label: "desk" }] },
    { id: 2, name: "Bruno Reis", favorite: false, numbers: [{ number: "1003", label: "" }] },
    { id: 3, name: "Support desk", favorite: true, numbers: [{ number: "600", label: "queue" }] },
    {
      id: 4,
      name: "Carla Nunes",
      favorite: false,
      numbers: [
        { number: "1004", label: "desk" },
        { number: "+5511555550104", label: "mobile" },
      ],
    },
    { id: 5, name: "Front door", favorite: false, numbers: [{ number: "*97", label: "intercom" }] },
  ];
  const past = (
    id: number,
    direction: HistoryEntry["direction"],
    remote: string,
    outcome: CallOutcome,
    ago: number,
    durationMs?: number,
  ): HistoryEntry => ({ id, direction, remote, outcome, startedUtc: now() - ago, durationMs });
  let calls: HistoryEntry[] = [
    past(16, "outgoing", "1002", "answered", 35 * 60, 84_000),
    past(15, "incoming", "1003", "missed", 3600),
    past(14, "incoming", "600", "answered", 3 * 3600, 192_000),
    past(13, "outgoing", "1003", "no_answer", 5 * 3600),
    past(12, "outgoing", "600", "answered", 2 * 86_400, 128_000),
    past(11, "incoming", "1002", "answered", 40 * 86_400, 48_000),
  ];

  const emit = (event: UiEvent) => handler(copy(event));
  const notice = (n: Notice) => emit({ type: "notice", notice: n });
  const later = (ms: number, run: () => void) => timers.push(setTimeout(run, ms));
  const register = (next: Registration) =>
    emit({ type: "registration", registration: (registration = next) });

  // Every change is a new snapshot with a higher version; the final one also writes history.
  function change(patch: Partial<CallSnapshot>): undefined {
    if (!call) return;
    call = { ...call, ...patch, version: call.version + 1 };
    if (call.phase === "established" && answeredAt === undefined) {
      answeredAt = Date.now();
      if (noAudio) notice("audio_unavailable");
    }
    emit({ type: "call", call });
  }
  function end(reason: EndReason): undefined {
    if (!call) return;
    timers.forEach(clearTimeout);
    timers = [];
    change({ phase: "ended", endReason: reason });
    const outcome: CallOutcome =
      answeredAt !== undefined
        ? "answered"
        : reason === "busy" || reason === "rejected" || reason === "canceled"
          ? reason
          : reason === "no_answer"
            ? call.direction === "incoming"
              ? "missed"
              : "no_answer"
            : "failed";
    calls.unshift({
      id: nextRow++,
      direction: call.direction,
      remote: call.remote,
      outcome,
      startedUtc,
      durationMs: answeredAt === undefined ? undefined : Date.now() - answeredAt,
    });
    call = undefined;
  }
  function start(
    direction: CallSnapshot["direction"],
    remote: string,
    phase: CallSnapshot["phase"],
  ) {
    answeredAt = undefined;
    startedUtc = now();
    call = {
      id: nextCall++,
      direction,
      remote,
      phase,
      muted: false,
      secure: account?.media === "srtp",
      version: 0,
    };
    change({});
  }
  function incoming(remote: string) {
    if (call) return notice("busy");
    if (saved.dnd) {
      calls.unshift(past(nextRow++, "incoming", remote, "rejected", 0));
      return;
    }
    start("incoming", remote, "ringing");
  }

  const connected = () => call?.phase === "established" || call?.phase === "held";
  function command(c: Command): Notice | undefined {
    if (failed) return "engine_failed";
    switch (c.type) {
      case "register": {
        account = c.account;
        if (c.password !== null) saved.hasPassword = true;
        register({ state: "registering" });
        const refused = c.password === "wrong";
        const down = c.account.server === "down.invalid";
        setTimeout(() => {
          if (registration.state !== "registering") return;
          register(
            refused
              ? { state: "failed", reason: "unauthorized" }
              : down
                ? { state: "failed", reason: "timeout" }
                : { state: "registered" },
          );
        }, 600);
        return;
      }
      case "unregister":
        register({ state: "disabled" });
        return;
      case "setDnd":
        saved.dnd = c.on;
        return;
      case "quit":
        if (call) return "busy";
      // falls through
      case "shutdown":
        end("normal");
        register({ state: "disabled" });
        emit({ type: "stopped" });
        return;
      case "dial": {
        if (registration.state !== "registered") return "not_registered";
        if (call) return "busy";
        start("outgoing", c.target, "dialing");
        later(500, () => change({ phase: "ringing" }));
        const scripted: Record<string, EndReason> = {
          "1007": "not_acceptable",
          "1008": "no_answer",
          "1009": "busy",
        };
        const reason = scripted[c.target];
        if (reason) later(reason === "no_answer" ? 4000 : 1200, () => end(reason));
        else later(2000, () => change({ phase: "established" }));
        return;
      }
    }
    if (!call || call.id !== c.id) return "unknown_call";
    switch (c.type) {
      case "answer":
        if (call.direction !== "incoming" || call.phase !== "ringing") return "invalid_state";
        return change({ phase: "established" });
      case "reject":
        if (call.direction !== "incoming" || call.phase !== "ringing") return "invalid_state";
        return end("rejected");
      case "hangup": {
        const reason = connected()
          ? "normal"
          : call.direction === "incoming"
            ? "rejected"
            : "canceled";
        change({ phase: "terminating" });
        later(200, () => end(reason));
        return;
      }
      case "dtmf":
        return call.phase === "established" ? undefined : "invalid_state";
      case "hold":
        return call.phase === "established" ? change({ phase: "held" }) : "invalid_state";
      case "resume":
        return call.phase === "held" ? change({ phase: "established" }) : "invalid_state";
      case "setMute":
        return connected() ? change({ muted: c.muted }) : "invalid_state";
      case "transfer": {
        if (!connected()) return "invalid_state";
        change({ transfer: { state: "pending" } });
        const refused = c.target === "1009";
        later(1200, () => {
          change({ transfer: refused ? { state: "failed", code: 486 } : { state: "succeeded" } });
          if (!refused) later(300, () => end("transferred"));
        });
        return;
      }
    }
  }

  function simulate(id: string) {
    switch (id) {
      case "incoming":
        return incoming("1002");
      case "hostile":
        return incoming('<img src=x onerror="alert(1)"><b>Mallory</b>');
      case "active":
      case "held":
      case "muted":
      case "transfer-failed":
        if (call) return notice("busy");
        start("outgoing", "1002", "established");
        if (id === "held") change({ phase: "held" });
        if (id === "muted") change({ muted: true });
        if (id === "transfer-failed") change({ transfer: { state: "failed", code: 486 } });
        return;
      case "approval":
        approvals.push({
          id: `op-${nextRow++}`,
          requester: "mcp: desk-agent",
          description: "Dial 1003",
          deadline: Date.now() + 60_000,
        });
        return emit({ type: "approvals", approvals });
      case "register-failed":
        return register({ state: "failed", reason: "rejected", code: 403 });
      case "audio":
        noAudio = true;
        return notice("audio_unavailable");
      case "engine-failed":
        failed = true;
        end("failed");
        register({ state: "disabled" });
        return notice("engine_failed");
    }
  }
  const ok = () => Promise.resolve();
  const decide = (keep: (a: Approval) => boolean) => {
    approvals = approvals.filter(keep);
    emit({ type: "approvals", approvals });
  };

  if (new URLSearchParams(location.search).get("scene") === "no-engine") {
    const fail = () => Promise.reject(NO_ENGINE);
    // Not a thenable: a promise must be able to resolve with this object.
    return new Proxy({} as Backend, {
      get: (_, method) =>
        method === "then"
          ? undefined
          : method === "version"
            ? () => Promise.resolve("go-microsip mock")
            : method === "ready"
              ? ok
              : fail,
    });
  }
  const matches = (text: string, search: string) =>
    text.toLowerCase().includes(search.trim().toLowerCase());
  return {
    version: () => Promise.resolve("go-microsip mock (fictional data)"),
    ready: ok,
    subscribe: (h) => {
      handler = h;
      const scene = new URLSearchParams(location.search).get("scene");
      register(registration);
      if (scene) simulate(scene);
      return Promise.resolve(() => (handler = () => {}));
    },
    send: (c) => {
      const refused = command(c);
      return refused ? Promise.reject(refused) : ok();
    },
    settings: (): Promise<Settings> => Promise.resolve(copy({ account, ...saved })),
    saveSettings: (patch) => {
      if (patch.account !== undefined) account = patch.account;
      Object.assign(saved, { ...patch, account: undefined });
      try {
        localStorage.setItem(THEME_KEY, saved.theme);
      } catch {
        // not remembered
      }
      return ok();
    },
    audioDevices: () =>
      noAudio
        ? Promise.reject("No audio device is available")
        : Promise.resolve({
            inputs: [
              { id: "mock:mic", name: "Built-in microphone", isDefault: true },
              { id: "mock:headset-in", name: "USB headset microphone", isDefault: false },
            ],
            outputs: [
              { id: "mock:speakers", name: "Built-in speakers", isDefault: true },
              { id: "mock:headset-out", name: "USB headset", isDefault: false },
            ],
          }),
    contacts: (search) =>
      Promise.resolve(
        copy(
          people
            .filter(
              (p) => matches(p.name, search) || p.numbers.some((n) => matches(n.number, search)),
            )
            .sort(
              (a, b) => Number(b.favorite) - Number(a.favorite) || a.name.localeCompare(b.name),
            ),
        ),
      ),
    saveContact: (contact) => {
      const row = copy({ ...contact, id: contact.id || nextRow++ });
      people = [...people.filter((p) => p.id !== row.id), row];
      return Promise.resolve(copy(row));
    },
    deleteContact: (id) => {
      people = people.filter((p) => p.id !== id);
      return ok();
    },
    history: (filter, search) =>
      Promise.resolve(
        copy(
          calls.filter(
            (c) =>
              matches(c.remote, search) &&
              (filter === "all" ||
                (filter === "missed" ? c.outcome === "missed" : c.direction === filter)),
          ),
        ),
      ),
    deleteCall: (id) => {
      calls = calls.filter((c) => c.id !== id);
      return ok();
    },
    clearHistory: () => {
      calls = [];
      return ok();
    },
    approve: (id) => {
      const request = approvals.find((a) => a.id === id);
      decide((a) => a.id !== id);
      // The mock's one request is "Dial 1003".
      if (request) {
        const refused = command({ type: "dial", target: "1003" });
        if (refused) notice(refused);
      }
      return ok();
    },
    deny: (id) => {
      decide((a) => a.id !== id);
      return ok();
    },
    denyAll: () => {
      decide(() => false);
      return ok();
    },
    grants: () => Promise.resolve(copy(grants)),
    createGrant: (spec, bootstrap) => {
      const known = GRANT_SCOPES.map((scope) => scope.id as string);
      if (!spec.scopes.length || spec.scopes.some((scope) => !known.includes(scope)))
        return Promise.reject("choose at least one known permission");
      if (spec.minutes < 1 || spec.minutes > 60 || spec.quota > 100)
        return Promise.reject("validity must be 1 to 60 minutes and the call limit at most 100");
      if (
        [...spec.destinations, ...spec.incoming].some((n) => !/^\+?[A-Za-z0-9*#_.-]{1,64}$/.test(n))
      )
        return Promise.reject("every number must be a valid number or SIP user");
      const id = `grant-7-${String(grants.length + 1).padStart(24, "0")}`;
      grants.unshift({
        id,
        scopes: [...spec.scopes],
        destinations: [...spec.destinations],
        incoming: [...spec.incoming],
        expires: Date.now() + spec.minutes * 60_000,
        quota: spec.quota,
        used: 0,
        bootstrap,
      });
      audit.unshift({
        at: Date.now(),
        operation: "grant",
        decision: bootstrap ? "bootstrap issued" : "issued",
        detail: `${spec.scopes.length} scopes, ${spec.minutes} min`,
      });
      return Promise.resolve(
        bootstrap
          ? `~/.config/go-microsip/bootstrap/${id}.json`
          : `/run/user/1000/go-microsip/grants/${id}.grant`,
      );
    },
    revokeGrant: (id) => {
      grants = grants.filter((g) => g.id !== id);
      audit.unshift({ at: Date.now(), operation: "grant", decision: "revoked", detail: "-" });
      return ok();
    },
    auditLog: () => Promise.resolve(copy(audit)),
    clearAudit: () => {
      audit = [];
      return ok();
    },
    scenarios: () => Promise.resolve(SCENARIOS),
    simulate: (id) => {
      simulate(id);
      return ok();
    },
  };
}
