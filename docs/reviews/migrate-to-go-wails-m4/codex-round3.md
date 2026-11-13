SOUND WITH CHANGES — M4, tasks 6.3–6.6: round 2’s major findings are resolved; the saved Windows walkthrough supports the narrowed acceptance, with minor issues remaining.
SOUND WITH CHANGES — task 7.1: the reports support 8 FreePBX and 68 Asterisk scenarios passing; minor fencing and script-evidence issues remain.

## Round 2 findings

1. **Resolved.** `Owner.Grants` retains unsuccessfully revoked bootstraps; `AgentAccess.revoke` restores the error after refreshing. `TestABootstrapWhoseRevocationIsNotStoredStaysListed` checks listing, retry and subsequent service refusal. The component itself remains untested.
2. **Resolved.** Toast arguments now bind the run and call, and `pressed` requires an outstanding ringing entry. `TestAToastNamesTheRunAndTheCallInItsButtons` covers another run, repeated presses and closed calls.
3. **Resolved.** `desktop.ps1` now performs the missing operations and runs `NotifyTest`; `vm-windows11.log:46–54` records them and correctly leaves notification buttons unverified.
4. **Resolved.** `Subscribe` obtains approvals outside `a.mu`, including its deferred reconciliation. `TestAPageSubscribesWhileAnAgentChangesDoNotDisturb` exercises the conflicting paths concurrently.
5. **Partially resolved.** `userOf` rejects processes created after the socket query began; the Windows test checks that rejection. Earlier PID reuse remains possible and is explicitly documented.
6. **Resolved.** `Listen` removes only a socket. `TestAClientReachesTheOwnerOnWindows` separately checks preservation of a regular file and recovery from a genuine stale socket.
7. **Resolved.** `docs/agent-cli-mcp.md:53–57` now accurately distinguishes revocation from cancellation of already-authorized DTMF.
8. **Resolved.** The held-phone test observes the full interval and counts REGISTER requests before and after repeated `Begin`. Refused-bootstrap startup is still supported by `cmd/service.go` ordering, not a service-level packet-count test.

## New findings

All findings below are **minor under the requested calibration**. I found no demonstrated route exposing either PBX to the host’s LAN or Internet.

### 1. [7.1] Minor — FreePBX’s fence does not survive reboot or cover IPv6

**Files/functions:** `build/vm/freepbx.sh:51–58`, `setup`; `build/vm/Vagrantfile`, `freepbx`.

The fence is an in-memory IPv4 INPUT chain installed by an explicit setup command. There is no boot-time restoration or corresponding IPv6 restriction.

**Scenario:** Keep the configured VM, then reboot it. FreePBX’s services restart, but this script’s rules do not, allowing the previously excluded Windows VM to reach it over the internal network. Independently, an IPv6 listener—such as Apache listening on IPv6—is outside these rules even before reboot.

**Fix:** Restore a fail-closed fence before services become reachable and either filter IPv6 equivalently or disable it explicitly. Qualify the receipt’s “listed client only” claim until that lifecycle is covered. This is an internal-lab restriction failure, not demonstrated outside-lab exposure.

### 2. [6.3, 7.1] Minor — refreshing either live fence can leave it open after failure

**Files/functions:** `build/vm/pbx.sh:25–32`, `up`; `build/vm/freepbx.sh:53–58`, `setup`.

Both scripts flush the existing chain before successfully constructing its replacement.

**Scenario:** Rerun setup against an already-running PBX with a malformed client address. An `iptables` command fails before the final DROP is installed. `set -e` stops the script but leaves the live PBX with an empty or incomplete fence.

**Fix:** Construct a complete replacement chain, including its final DROP, before switching traffic to it; alternatively retain a temporary deny rule throughout replacement.

The existing bindings and VirtualBox network still contain this failure within the lab.

### 3. [6.3, 7.1] Minor — failed probe execution can be reported as successful isolation

**Files/functions:** `build/vm/run.sh:22,49`, `reaches`; `build/vm/interop.sh:39–43`, negative fence check; `build/vm/pbx.sh:38–56`, `ring`/`report`.

The negative connection checks interpret “no `True` in output” as a completed unsuccessful connection attempt. They do not distinguish that from WinRM or PowerShell failing to run the probe.

**Scenario:** Windows is reported running, but WinRM fails. `interop.sh` prints that the unlisted VM gets no connection, and successful scenarios from the auxiliary VM can still make the run exit zero. Similarly, a failed originate command without the specific text “No such command” can produce “the PBX called”; an `iptables` failure can be hidden by the report pipeline’s successful `awk`.

**Fix:** Check the producer’s exit status separately, require an explicit Boolean probe result, and report execution failures as failures or NOT VERIFIED. Check originate/report command status before interpreting their output.

This does not prove that the saved probes failed; it means their summary lines alone cannot establish successful probe execution.

### 4. [6.3, 7.1] Minor — early failures bypass teardown

**Files/functions:** `build/vm/run.sh`, top-level orchestration; `build/vm/interop.sh:25–26,56–58`.

Cleanup occurs at the bottom rather than through an exit trap; destruction failures are also discarded.

**Scenario:** With `KEEP` unset, bring up the auxiliary VM, then fail FreePBX provisioning or a later setup command. `set -e` exits before teardown, leaving VMs and their fictional credentials behind. An early exit in `run.sh` can likewise bypass background-job cleanup.

**Fix:** Install cleanup traps before creating resources, preserve the original failure status, reap owned background jobs, and report failed destruction.

The FreePBX receipt honestly says teardown was not exercised, but “destroys both VMs after the run unless `KEEP=1`” is too unconditional.

### 5. [6.3] Minor — keyboard delivery and firewall attribution are not sufficiently guarded

**Files/functions:** `build/vm/window.ps1:69–90`, `Front`/`Key`; `build/vm/call.ps1:47–59`, `Asked`.

`Front` only prints a warning when foreground acquisition fails; `Key` sends input anyway. `Asked` infers a firewall question from newly observed rules, ignores the first `Fronted` result, and sends Escape without identifying the foreground dialog.

**Scenario:** Another window retains focus despite the foreground attempts. Keys go there. In `Asked`, Escape can dismiss an unrelated notification or dialog, after which foreground acquisition succeeds and the script reports that Windows Firewall’s Cancel was pressed.

**Fix:** Refuse keyboard input when foreground ownership is unconfirmed. Identify the firewall dialog before dismissing it, or mark that step NOT VERIFIED without sending Escape.

`AttachThreadInput` is detached on the normal path, but it does not establish which dialog received Escape. The receipt’s definite firewall-Cancel claim is stronger than this check alone establishes.

### 6. [6.3] Minor — the toast-button script can claim a decision without receiving one

**Files/functions:** `build/vm/desktop.ps1:134–169`, notification check; `internal/platform/notify/notify_windows_test.go:24–35`, `TestAToastOnThisDesktop`.

The script searches the whole desktop for any button named Answer. After invoking one, it accepts test exit zero as proof of callback delivery. However, the test permits a callback timeout unless `GO_MICROSIP_NOTIFY_PRESSED` is set; the script never sets it.

**Scenario:** An unrelated Answer button is invoked, or toast activation is broken. The test logs NOT VERIFIED and exits zero, while `$pressed` makes the script report that the toast decided the call.

**Fix:** Scope the button to the identified notification and require the matching callback acknowledgement before reporting a decision.

The supplied run did **not** take this false-success branch: it explicitly records buttons as unverified.

### 7. [6.3] Minor — the call walkthrough can “verify removal” of a notification never shown

**File/functions:** `build/vm/call.ps1:102–121`, incoming-call notification checks.

The ringing history count is printed but not required to be positive. History-query exceptions also do not fail the step.

**Scenario:** The packaged application’s notifier fails to submit its toast. History contains zero notifications while ringing and zero afterward; the script still reports that its notification is gone.

**Fix:** Establish a fresh notification before testing its removal, and distinguish unreadable history from a successful check.

The saved run’s **one-to-zero** history transition supports removal from notification history. It does not independently prove immediate disappearance from the screen at answer time: the second query occurs after hang-up.

### 8. [6.3] Minor — failed toast removal is forgotten permanently

**Files/functions:** `internal/platform/notify/notify.go:85–90`, `closed`; `notify_windows.go:101–102`, `serve`.

`closed` deletes the entry before `forget` runs, and `serve` discards a removal error. Subsequent closes cannot retry it.

**Scenario:** Windows temporarily refuses the history operation when a call is answered. The old notification remains, potentially indefinitely in notification history, but the notifier no longer tracks its cleanup.

**Fix:** Keep cleanup state separate from permission to decide the call, and retry removal safely. Because removal is application-wide, do not retry an old clear after a newer call’s toast has been shown; serialize cleanup before that show or use notification-specific removal.

The remaining toast is inert: this does **not** reopen round 2’s stale-call authorization defect.

### 9. [6.3] Minor — every clear adds an unmatched Windows Runtime initialization

**File/function:** `internal/platform/notify/notify_windows.go:58–59`, `forget`.

Every removal calls `RoInitialize`, but none balances a successful initialization, including `S_FALSE`.

**Scenario:** A long-running session clears hundreds of calls and accumulates hundreds of unmatched initialization references on its notifier thread.

**Fix:** Initialize once on the locked serving thread, or balance each successful call after releasing its objects. Microsoft explicitly requires balancing successful calls, including `S_FALSE`. This is a lifetime-management issue, not a demonstrated softphone crash. [RoInitialize contract](https://learn.microsoft.com/en-us/windows/win32/api/roapi/nf-roapi-roinitialize)

## Checked and found supported

- **Asterisk’s normal fence and bindings:** the published ports name the isolated IPv4 address explicitly. RTP counting rules precede the general client RETURN rules; the final DROP blocks other forwarded traffic, including traffic through the auxiliary VM’s other interface. `DOCKER-USER` is processed before Docker’s forwarding accepts. [Docker firewall ordering](https://docs.docker.com/engine/network/firewall-iptables)
- **Restart distinction:** Docker deliberately preserves existing `DOCKER-USER` rules across daemon restarts. A VM reboot loses these volatile rules, but this Compose configuration has no automatic container restart policy. Thus reboot does not automatically restart an unfenced Asterisk PBX. The separate NAT adapter is not a bridged LAN adapter; internal networking is not host-accessible. [Docker implementation](https://github.com/moby/moby/blob/v26.1.5/libnetwork/firewall_linux.go), [VirtualBox networking](https://docs.oracle.com/en/virtualization/virtualbox/7.0/user/networkingdetails.html)
- **Configuration rendering:** `LAB_BIND_IP` cannot inject PJSIP directives: it is parsed and rendered as a canonical IPv4 address. Wildcard, loopback, link-local, public and newline-injection examples are rejected by the named tests. `own_address` supplies the container address for the `/32` local network; the external addresses remain the isolated VM address.
- **Installer pinning:** the Vagrantfile uses a commit-specific HTTPS URL and checks its SHA-256 before execution. `--skipversion` skips the upstream freshness check; it does not select a replacement installer. This pins the installer script, not every package it downloads. [Pinned upstream script](https://raw.githubusercontent.com/FreePBX/sng_freepbx_debian_install/b023b8426786a518d25dfc0d11ba0c0314fcd927/sng_freepbx_debian_install.sh)
- **Passwords and paths:** no apparent non-fictional credential or personal path was found in the reviewed additions or supplied logs. FreePBX’s import CSV is created privately and removed on ordinary exit, including command-error exit. Its intended persistent extension configuration still contains the fictional passwords. `call.ps1` removes the password environment variable after launching the application; neither its command line nor its normal log prints that password.
- **WinRT ABI:** both interface identifiers are correct. With the six inherited `IInspectable` slots, History is slot 6 and ClearWithId is slot 12. Manager, returned object, queried interface and HSTRING lifetimes are otherwise balanced. Production removal runs on the locked serving thread. [Microsoft interface definitions](https://github.com/microsoft/windows-rs/blob/master/crates/libs/windows/src/Windows/UI/Notifications/mod.rs)
- **Toast state:** closing an unknown call does not invoke WinRT. Failed submission removes the ringing entry. Pressed actions are accepted once; ordinary button activation dismisses the notification by default. The normal queued sequence—close A, then show B—is serialized, so A’s synchronous clear does not clear the subsequently shown B. [Windows action behavior](https://learn.microsoft.com/en-us/uwp/schemas/tiles/toastschema/element-action)
- **Audio cleanup:** registering cleanup before attempting Disable fixes the refusal path that previously skipped restoration. The saved log does not establish successful hardware restoration, and correctly reports unplug as unverified.
- **M4 receipt and task 6.3:** the log identifies executable commit `6107125eafce`, records the restored desktop operations and all three call walkthroughs, and ends with **FAIL: audio**, as the Runs section acknowledges. The PBX log supports the aggregate 1710/1709 packet counts—not audible quality or separate media measurements for each call. The owner-approved move of hardware audio and notification buttons to 7.2 is explicit; checking narrowed task 6.3 is not represented as passing the original hardware gate.
- **Task 7.1:** the JSON reports contain the stated 8 and 68 successful scenarios. The FreePBX results substantiate echo in both codecs, distinct tones between extensions, hold/resume, mute, transfer destination audio, and correct/wrong DTMF controls. The receipt accurately limits this to engine-level UDP interoperability and discloses the NOTIFY/unregistration warnings, missing packaged-application coverage and untested teardown.

**Not checked:** no builds, tests, repository scripts, applications or VMs were run; omitted screenshots, binaries, provision logs, engine/dependency implementations and actual artifact/installer hashes were not independently validated.  
Real-hardware Windows audio and actual notification-button operation remain unverified work for task 7.2.