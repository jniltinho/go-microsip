// Ported from the tests of apps/rust-microsip/src/ui/state.rs and crates/core/src/lib.rs. The Rust
// tests of storage, keyring, CSV import and the audio test belong to the Go core, not to this page.
import { expect, test } from "vite-plus/test";
import type {
  AccountSettings,
  CallPhase,
  CallSnapshot,
  Command,
  Direction,
  EndReason,
  Notice,
} from "./api";
import {
  type Action,
  APPROVAL_ARM,
  ageText,
  defaultPort,
  formatDuration,
  NOTICE_TTL,
  noticeText,
  parseDialTarget,
  registrationText,
  UiState,
  validateAccount,
  validationText,
} from "./state";

class Fake {
  sent: string[] = [];
  saved: string[] = [];
  fail: Notice | null = null;
  send = (c: Command) => {
    if (this.fail) return Promise.reject(this.fail);
    const { type, ...rest } = c.type === "register" ? { ...c, password: "[REDACTED]" } : c;
    this.sent.push(`${type}(${Object.values(rest).map(String).join(", ")})`);
    return Promise.resolve();
  };
  saveSettings = (patch: object) => Promise.resolve(void this.saved.push(JSON.stringify(patch)));
  deleteCall = (id: number) => Promise.resolve(void this.saved.push(`deleteCall(${id})`));
  clearHistory = () => Promise.resolve(void this.saved.push("clearHistory"));
  deleteContact = (id: number) => Promise.resolve(void this.saved.push(`deleteContact(${id})`));
  approve = (id: string) => Promise.resolve(void this.saved.push(`approve(${id})`));
  deny = (id: string) => Promise.resolve(void this.saved.push(`deny(${id})`));
  denyAll = () => Promise.resolve(void this.saved.push("denyAll"));
  take() {
    return this.sent.splice(0);
  }
}

const fresh = (): [UiState, Fake] => {
  const b = new Fake();
  return [new UiState(b), b];
};
const snap = (
  id: number,
  direction: Direction,
  phase: CallPhase,
  version: number,
  endReason?: EndReason,
) =>
  ({
    id,
    direction,
    remote: "1002",
    phase,
    endReason,
    muted: false,
    secure: true,
    version,
  }) as CallSnapshot;
const call = (st: UiState, s: CallSnapshot, now = 0) =>
  st.applyEvent({ type: "call", call: s }, now);
async function run(st: UiState, actions: (Action | Action["type"])[], now = 0) {
  for (const a of actions)
    await st.dispatch(typeof a === "string" ? ({ type: a } as Action) : a, now);
}
const key = (k: string): Action => ({ type: "key", key: k });

test("dial validates the number before sending", async () => {
  const [st, b] = fresh();
  expect(st.cannotCall).toBe("Enter a number first");
  st.number = "10 02";
  await run(st, ["call"]);
  expect(b.take()).toEqual([]);
  expect(st.notice(0)).toBe(validationText("invalidCharacters"));
  st.number = "";
  await run(st, [key("1"), key("x"), key("0"), "plus", "backspace", key("0"), key("2"), "call"]);
  expect(st.number).toBe("1002");
  expect(b.take()).toEqual(["dial(1002)"]);
});

test("keypad sends DTMF only when established", async () => {
  const [st, b] = fresh();
  call(st, snap(7, "outgoing", "ringing", 1));
  await run(st, [key("5")]);
  expect(b.take()).toEqual([]);
  expect(st.number).toBe("5");
  call(st, snap(7, "outgoing", "established", 2));
  expect(st.keypadSendsDtmf).toBe(true);
  await run(st, [key("9"), key("#"), key("a")]);
  expect(b.take()).toEqual(["dtmf(7, 9)", "dtmf(7, #)", "dtmf(7, A)"]);
  expect(st.number).toBe("5");
  call(st, snap(7, "outgoing", "held", 3));
  await run(st, [key("1")]);
  expect(b.take()).toEqual([]);
});

test("incoming banner answers and rejects", async () => {
  const [st, b] = fresh();
  call(st, snap(3, "incoming", "ringing", 1));
  expect([st.callLabel, st.endLabel]).toEqual(["Answer", "Reject"]);
  expect(st.cannotCall).toBeNull();
  await run(st, ["call", "end"]);
  expect(b.take()).toEqual(["answer(3)", "reject(3)"]);
});

test("hold and mute follow snapshots, not clicks", async () => {
  const [st, b] = fresh();
  await run(st, ["toggleMute", "toggleHold"]);
  expect(b.take()).toEqual([]);
  call(st, snap(1, "outgoing", "established", 1));
  await run(st, ["toggleMute", "toggleHold"]);
  expect(b.take()).toEqual(["setMute(1, true)", "hold(1)"]);
  // Nothing changed on screen until the engine says so.
  expect(st.call?.snapshot).toMatchObject({ muted: false, phase: "established" });
  call(st, { ...snap(1, "outgoing", "held", 2), muted: true });
  await run(st, ["toggleMute", "toggleHold", "end"]);
  expect(b.take()).toEqual(["setMute(1, false)", "resume(1)", "hangup(1)"]);
});

test("DND and the transfer prompt", async () => {
  const [st, b] = fresh();
  await run(st, ["toggleDnd", "openTransfer"]);
  expect(st.dnd).toBe(true);
  expect(st.transferTarget).toBeNull();
  expect(b.take()).toEqual(["setDnd(true)"]);
  call(st, snap(4, "outgoing", "established", 1));
  await run(st, ["openTransfer"]);
  st.transferTarget = "bad target";
  await run(st, ["confirmTransfer"]);
  expect(b.take()).toEqual([]);
  expect(st.transferTarget).not.toBeNull();
  st.transferTarget = "1003";
  await run(st, ["confirmTransfer"]);
  expect(b.take()).toEqual(["transfer(4, 1003)"]);
  expect(st.transferTarget).toBeNull();
});

test("engine failure is sticky in the status", async () => {
  const [st, b] = fresh();
  st.applyEvent({ type: "registration", registration: { state: "registered" } });
  expect(st.engineFailed).toBe(false);
  b.fail = "engine_failed";
  await run(st, ["toggleDnd"]);
  expect([st.engineFailed, st.dnd]).toEqual([true, false]);
  st.applyEvent({ type: "notice", notice: "busy" });
  expect(st.engineFailed, "later notices do not clear the failure").toBe(true);
  const [quit] = fresh();
  quit.applyEvent({ type: "stopped" });
  quit.applyEvent({ type: "notice", notice: "engine_failed" });
  expect(quit.engineFailed, "a closed queue after a normal stop is expected").toBe(false);
});

test("overloaded backend shows a notice and keeps DND", async () => {
  const [st, b] = fresh();
  b.fail = "overloaded";
  await run(st, ["toggleDnd"]);
  expect(st.dnd).toBe(false);
  expect(st.notice(0)).toBe(noticeText("overloaded"));
});

test("events dedup by version and the final snapshot leaves the call", async () => {
  const [st] = fresh();
  st.applyEvent({ type: "registration", registration: { state: "registered" } });
  expect(registrationText(st.registration)).toBe("Registered");
  call(st, snap(9, "outgoing", "established", 2));
  // Stale and duplicate snapshots are ignored.
  call(st, snap(9, "outgoing", "dialing", 1));
  call(st, snap(9, "outgoing", "held", 2));
  expect(st.call?.snapshot.phase).toBe("established");
  // A second call does not replace the current one.
  call(st, snap(10, "incoming", "ringing", 1));
  expect(st.call?.snapshot.id).toBe(9);
  expect(formatDuration(st.duration(65_000) ?? 0)).toBe("1:05");
  call(st, snap(9, "outgoing", "ended", 3, "normal"), 65_000);
  expect(st.call).toBeNull();
  expect(st.historyRevision).toBe(1);
  call(st, snap(9, "outgoing", "ended", 3, "normal"), 65_000);
  expect(st.historyRevision, "a repeated final snapshot is not a new history entry").toBe(1);
  st.tab = "calls";
  await run(st, [{ type: "fillNumber", number: "1002" }]);
  expect([st.tab, st.number]).toEqual(["phone", "1002"]);
  st.applyEvent({
    type: "registration",
    registration: { state: "failed", reason: "rejected", code: 403 },
  });
  expect(registrationText(st.registration)).toBe("Failed: rejected (403)");
  st.setNotice("x", 0);
  expect(st.notice(NOTICE_TTL - 1)).toBe("x");
  expect(st.notice(NOTICE_TTL)).toBeNull();
});

test("redial from history needs an explicit action", async () => {
  const [st, b] = fresh();
  await run(st, [{ type: "fillNumber", number: "1002" }]);
  expect(b.take(), "filling the number never dials").toEqual([]);
  await run(st, [{ type: "dialNumber", number: "1003" }]);
  expect(b.take()).toEqual(["dial(1003)"]);
  call(st, snap(1, "outgoing", "established", 1));
  await run(st, [{ type: "dialNumber", number: "1004" }]);
  expect(b.take()).toEqual([]);
  expect(st.notice(0)).toBe("A call is already active");
});

test("quitting during a call asks first and signals do not", async () => {
  let [st, b] = fresh();
  call(st, snap(1, "outgoing", "established", 1));
  await run(st, ["quit"]);
  expect(st.confirm).toEqual({ kind: "quit" });
  expect([st.quitting, b.take()]).toEqual([false, []]);
  await run(st, ["cancelConfirm"]);
  expect([st.quitting, st.call !== null]).toEqual([false, true]);
  await run(st, ["quit", "confirm"]);
  expect([st.quitting, st.confirm]).toEqual([true, null]);
  expect(b.take()).toEqual(["shutdown()"]);
  // SIGTERM path: no prompt.
  [st, b] = fresh();
  call(st, snap(2, "outgoing", "established", 1));
  await run(st, ["quitNow"]);
  expect([st.quitting, st.confirm]).toEqual([true, null]);
  expect(b.take()).toEqual(["shutdown()"]);
  // Without a call, quitting is immediate.
  [st, b] = fresh();
  await run(st, ["quit", "quit"]);
  expect(st.quitting).toBe(true);
  expect(b.take()).toEqual(["shutdown()"]);
});

test("switching the skin during a call leaves the call alone", async () => {
  const [st, b] = fresh();
  call(st, snap(7, "outgoing", "established", 1));
  for (const theme of ["dark", "win98", "system"]) {
    await run(st, [{ type: "setTheme", theme }]);
    expect(st.theme).toBe(theme);
  }
  expect(b.take(), "a skin change sends no engine command").toEqual([]);
  expect(b.saved).toEqual(['{"theme":"dark"}', '{"theme":"win98"}', '{"theme":"system"}']);
  expect(st.keypadSendsDtmf).toBe(true);
});

test("destructive actions wait for confirmation", async () => {
  const [st, b] = fresh();
  await run(st, [{ type: "deleteCall", id: 5 }]);
  expect(b.saved).toEqual([]);
  await run(st, [
    "cancelConfirm",
    "clearHistory",
    "confirm",
    { type: "deleteContact", id: 2 },
    "confirm",
  ]);
  expect(b.saved).toEqual(["clearHistory", "deleteContact(2)"]);
  expect([st.historyRevision, st.contactsRevision]).toEqual([1, 1]);
});

test("an agent request arms after a second and deny always works", async () => {
  const [st, b] = fresh();
  const approvals = [
    { id: "a", requester: "mcp", description: "Dial 1003", deadline: 60_000 },
    { id: "b", requester: "mcp", description: "Dial 1004", deadline: 60_000 },
  ];
  st.applyEvent({ type: "approvals", approvals }, 100);
  await run(st, [{ type: "approve", id: "a" }], 100 + APPROVAL_ARM - 1);
  expect(b.saved, "a click that lands too early approves nothing").toEqual([]);
  await run(st, [{ type: "approve", id: "b" }], 5000);
  expect(b.saved, "only the request on screen can be approved").toEqual([]);
  await run(st, [{ type: "approve", id: "a" }], 100 + APPROVAL_ARM);
  expect(b.saved).toEqual(["approve(a)"]);
  // The next request restarts the clock.
  st.applyEvent({ type: "approvals", approvals: approvals.slice(1) }, 2000);
  expect(st.approvalArmed(2500)).toBe(false);
  await run(st, [{ type: "deny", id: "b" }, "denyAll"], 2000);
  expect(b.saved.slice(1)).toEqual(["deny(b)", "denyAll"]);
  st.applyEvent({ type: "approvals", approvals: [] }, 3000);
  expect(st.approvalShown).toBeNull();
});

const account = (): AccountSettings => ({
  displayName: "Lab",
  username: "1001",
  authUsername: "",
  domain: "pbx.invalid",
  server: "192.0.2.1",
  port: 5060,
  transport: "udp",
  media: "rtp",
  expires: 300,
});

test("account validation rejects bad fields and SRTP without TLS", () => {
  expect(validateAccount(account())).toBeNull();
  expect(validateAccount({ ...account(), username: "" })).toBe("empty");
  expect(validateAccount({ ...account(), server: "evil host" })).toBe("invalidCharacters");
  expect(validateAccount({ ...account(), port: 0 })).toBe("invalidPort");
  expect(validateAccount({ ...account(), port: 70_000 })).toBe("invalidPort");
  expect(validateAccount({ ...account(), media: "srtp" })).toBe("srtpRequiresTls");
  expect(validateAccount({ ...account(), media: "srtp", transport: "tls" })).toBeNull();
  expect(validateAccount({ ...account(), displayName: 'a"b' })).toBe("invalidCharacters");
  expect(validateAccount({ ...account(), displayName: "abc\\" })).toBe("invalidCharacters");
  expect([defaultPort("udp"), defaultPort("tcp"), defaultPort("tls")]).toEqual([5060, 5060, 5061]);
});

test("settings validate, save and register", async () => {
  const [st, b] = fresh();
  await run(st, ["register"]);
  expect([st.view, b.take()], "no account yet: settings open instead").toEqual(["settings", []]);
  const save = (a: AccountSettings, password: string | null): Action => ({
    type: "saveAccount",
    account: a,
    password,
  });
  await run(st, [save({ ...account(), media: "srtp" }, "s3cret-marker")]);
  expect(st.notice(0)).toBe("SRTP requires TLS signaling");
  const tls: AccountSettings = {
    ...account(),
    media: "srtp",
    transport: "tls",
    port: defaultPort("tls"),
  };
  await run(st, [save(tls, null)]);
  expect(st.notice(0)).toBe("Password is required");
  expect([b.saved, b.take()]).toEqual([[], []]);
  await run(st, [save(tls, "s3cret-marker")]);
  expect([st.view, st.hasPassword, st.accountText]).toEqual(["main", true, "1001@pbx.invalid"]);
  const sent = b.take();
  expect(sent).toHaveLength(1);
  expect(sent[0]).toMatch(/^register\(/);
  expect(sent[0] + b.saved.join(), "the password is never printed or saved").not.toContain(
    "s3cret-marker",
  );
  expect(b.saved).toHaveLength(1);
  // Re-saving without retyping reuses the password the backend holds.
  await run(st, ["openSettings", save(tls, null), "register"]);
  expect(b.take()).toHaveLength(2);
});

test("dial targets are bounded", () => {
  for (const ok of ["1002", "*97", "+5511999990000", "alice.b"])
    expect(parseDialTarget(ok).ok, ok).toBe(true);
  for (const bad of ["", "10 02", "sip:1002@x", "1+2", "a;b", "9".repeat(65), "1002\r\nX"])
    expect(parseDialTarget(bad).ok, JSON.stringify(bad)).toBe(false);
  expect(parseDialTarget(" 1002 ")).toEqual({ ok: true, value: "1002" });
});

test("ages and durations", () => {
  expect([ageText(5), ageText(125), ageText(7300), ageText(200_000)]).toEqual([
    "just now",
    "2 min ago",
    "2 h ago",
    "2 d ago",
  ]);
  expect([formatDuration(5000), formatDuration(3_725_000)]).toEqual(["0:05", "1:02:05"]);
});
