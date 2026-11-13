# SOUND WITH CHANGES

**Ready to put to the owner for staged approval.** N01 is resolved at design level. The remaining changes below are small specification/task corrections, not reasons to demand implementation evidence before task 1.1. Group 2 measures feasibility; task 2.7 requires revised decisions before production work proceeds.

“Resolved” means **resolved in the plan**, not implemented or tested.

## Status

| Finding | Status | Assessment against the revised text |
|---|---|---|
| N01 | resolved | D4 defines immutable queue-instance ownership, atomic check/publication, retirement before disposal, replacement and fail-closed behavior. Tasks 2.3/3.3 cover adversarial interleavings. |
| N02 | resolved | Portability moves to 1.4; the packaged audio prototype is 2.3, before permission and network measurements. |
| N03 | resolved | D4 explicitly stages the x86_64 decision. Task 2.8 supplies arm64 measurements; 7.3 remains blocked without them. |
| N04 | resolved | D5 removes variable pathname operands, refuses empty values, bounds the command and specifies one command followed by EOF, with separate output streams. |
| N05 | resolved | D5/4.3 require the native decoy control and replacement by the framework fallback if selection is not fail-closed. |
| N06 | resolved | D10 covers outer output and retained files, including failure and interruption; 1.4 adds corresponding controls. |
| N07 | resolved | Portable command construction/parsing runs in Docker; Darwin wiring and real-tool acceptance run on the Mac. |
| N08 | resolved | D6 accurately narrows the inherited checks and separates Darwin pathname/ACL validation. Tasks 4.4–4.5 preserve Linux behavior. |
| N09 | resolved | The misleading OS-name seam is dropped. Darwin paths are tested natively; existing platform branches remain untouched. |
| N10 | resolved | Proposal/D7 declare additive diagnostic fields; `audio.available` retains its existing meaning. |
| N11 | partly | The unsupported runtime hint is removed. Measurement and limits have tasks, but the new troubleshooting requirement lacks an explicit delivery/check. See P01. |
| N12 | resolved | D12/6.4 now test omitted labels using production defaults, complete notes and a self-consistent legacy artifact set. |
| N13 | partly | D12/7.2–7.3 require mandatory passes and narrowly permit notification-button N/A. The distribution requirement still needs that exception. See P02. |
| N14 | resolved | Both the CI requirement and “Green run” scenario now qualify coverage by hosted availability. |
| F06 leftovers | resolved | D4/2.4/3.5 cover cancellation and quit while authorization is pending; 3.1 explicitly pins the window’s Windows notice. |
| F19 leftovers | resolved | The integration requirement expressly qualifies the migration’s navigation rule. The proposal acknowledges it; 1.1 requests owner acceptance. |
| F20 leftovers | resolved | D13 and tasks 2.6, 5.5, 7.2 explicitly include hidden-idle incoming calls, audio-service restart and Bluetooth profile/rate transition. |

## D4: AudioQueue discard assessment

**The replacement-queue design is sound as a plan-level candidate.** I no longer see N01’s stale-publication race:

- A callback retains its original instance identity.
- Publication either completes before retirement and is subsequently cleared, or observes retirement and drops its samples.
- Old partial buffers and queue-owned conversion state cannot become publications from the replacement instance.

This fits Apple’s documented behavior: stopping can reset the queue and invoke callbacks, while immediate disposal is synchronous, releases queue resources and prevents subsequent callbacks. Retiring first and performing native teardown outside the publication lock is therefore appropriate. I found **no documented AudioQueue behavior that defeats this protocol**. [AudioQueueStop](https://developer.apple.com/documentation/audiotoolbox/audioqueuestop(_:_:)), [AudioQueueReset](https://developer.apple.com/documentation/audiotoolbox/audioqueuereset(_:)), [AudioQueueDispose](https://developer.apple.com/documentation/audiotoolbox/audioqueuedispose(_:_:))

The implementation must still enforce the stated lifecycle invariants: no retired callback re-enqueues, callback state remains alive through synchronous disposal, and a pending reopen cannot resurrect capture after `Close`. The publication lock alone does not establish those properties.

**The listed tests are an adequate acceptance outline**, provided the disposal/close cases assert those properties and exercise real AudioQueue behavior—not only simulated callback delivery. Native timing, reopening cost and freshness on actual devices remain measurements, not established facts.

## New findings

References are under `openspec/changes/add-macos-universal/` unless otherwise stated.

| Id | Severity | Section | Concrete scenario | Fix |
|---|---|---|---|---|
| P01 | minor | Integration “A PBX on the local network”; tasks 5.8/8.1 | All named documentation tasks can finish without adding the mandatory Local Network troubleshooting advice: 5.8 covers desktop integration; 8.1 names platform tables and limits. | Add the troubleshooting paragraph and its verification explicitly to 8.1. |
| P02 | minor | Distribution “Gates of a final release”; D12; 7.2–7.3 | macOS refuses notifications; the documented fallback passes and buttons receive the permitted N/A, but the distribution requirement still unconditionally demands the macOS rows “passed on each architecture.” | State mandatory-row passes plus the narrow, evidenced notification-button exception in that requirement. |
| P03 | minor | Task 6.4 | “Each control fails before its fix” cannot literally apply to the forbidden-file ZIP control: the unchanged scanner already scans ZIP contents and has such negative controls. | Require before/after failure evidence for newly repaired enforcement; treat existing ZIP scanning as a regression control, or demonstrate it with a deliberate mutation. |

## Checked and found supported

- No design/spec task-number citation points to an unrelated revised task.
- Prototype, permission and network measurements now have a workable dependency order.
- Native hardware acceptance remains separate from hosted smoke checks and VM measurements.
- Unsigned distribution is consistently accepted; no signing change is needed.
- Final-release gates require successful mandatory hardware results and cover the atomic artifact transition.

## Not checked

No builds, tests, scripts, binaries, SSH connections, VM operations or calls were executed; no files were changed. Additional ai-memory retrieval was unavailable.

**This is a static plan review, not evidence that anything builds, runs or carries a call.** AudioQueue/purego feasibility and unsigned permission behavior remain genuine execution risks.