# Go migration, M5: FreePBX 17 interoperability (task 7.1)

Change `openspec/changes/migrate-to-go-wails`, task 7.1. The lab gets FreePBX 17 as its interoperability target, and the engine's scenarios (registration, calls, DTMF, hold, blind transfer) pass against it and against the Asterisk lab. This is engine evidence: the packaged application against a PBX from a desktop is the Windows walkthrough of task 6.3 (Asterisk) and the manual checklist of task 7.2. It is not acceptance of a release.

## A VM, not a container

By the owner's decision of 2026-11-12 the target is a disposable VM, not the community container image the design first named: FreePBX 17 is installed by Sangoma's own installer on a Debian 12 machine. The task, the design, the proposal and the specification say so now.

- **The VM** (`freepbx` in `build/vm/Vagrantfile`): `bento/debian-12`, 2 GB, on the VirtualBox internal network of the VM lab (192.168.77.11), which the host has no adapter on, beside the auxiliary `pbx` VM (192.168.77.10).
- **The installer** is fetched at one commit of `FreePBX/sng_freepbx_debian_install` (`b023b8426786a518d25dfc0d11ba0c0314fcd927`, script version 1.15) and refused unless its SHA-256 is `afef5e4b480cf545b2035f92068dc2fdd32452989d170a16a84acb6e37b7d564`. It runs with `--skipversion` (it would otherwise stop when a newer script exists) and `--nochrony`.
- **Not open-source modules only.** The installer's `--opensourceonly` was tried first and stopped the installation half way, with status 123, where it removes commercial modules: one that is listed is not installed. The VM was destroyed and installed again as the installer's documentation runs it. The commercial modules it brings are neither activated nor used here.
- **What is installed**: FreePBX 17.0.33.4 (`fwconsole`), Asterisk 22.11.0, Debian 12; one SIP transport, UDP on 5060, as installed.
- **Extensions and routes** (`build/vm/freepbx.sh setup`): the lab's fictional accounts 1001 and 1002, with the lab's generated passwords, through FreePBX's own bulk import, so the calls take FreePBX's generated dialplan and its defaults for an extension (among them direct media, symmetric RTP, RFC 4733 DTMF); and the lab's three test routes (600 echo, 601 the three-digit question, 602 a steady tone) in the file FreePBX keeps for an administrator's dialplan.
- **The fence**: on the lab's network the VM accepts the listed client only, the auxiliary VM the scenarios run from. The run shows both sides: the listed client reaches FreePBX's web port, and the Windows VM, on the same network and not listed, gets no connection.
- **Teardown**: `build/vm/interop.sh` destroys both VMs after the run unless `KEEP=1`. The VMs are kept for now by the owner's decision, so that path was not run; `vagrant destroy -f freepbx`, the same command, removed the half-installed VM.

## Results

`make go-freepbx` builds the scenario runner (`g0lab`, without cgo) and runs it on the auxiliary VM against FreePBX, the Asterisk lab on that VM stopped. `make go-lab` runs every scenario against the Asterisk lab in its internal Docker network.

| Scenario | FreePBX 17 (UDP) | Asterisk lab |
| --- | --- | --- |
| registration (`register/1001`) | pass | pass (UDP, TCP, TLS) |
| a call with audio both ways through the PBX's echo (`engine/echo/1001/PCMA`, `/PCMU`) | pass in both codecs: the tone sent is the tone heard | pass |
| a call between two extensions (`engine/pair/1001-1002`) | pass: answered, each side hears the other's tone | pass (also 1201-1202, TLS with SRTP) |
| hold and mute (`engine/hold-mute/1001-1002`) | pass: neither side hears the other's tone while held, both do before and after; the muted side is not heard. FreePBX sends its music to the held side (76 frames arrive), which the scenario does not take for the tone | pass |
| blind transfer (`engine/transfer/1001-1002`) | pass: the transfer is reported succeeded and the transferred side hears the tone of route 602 | pass |
| DTMF (`engine/dtmf/1001`, and its control with wrong digits) | pass: the three digits sent as RFC 4733 events are read by the PBX, which answers with the accepting tone; wrong digits get the rejecting one | pass |
| do-not-disturb during a call (`engine/dnd/during-a-call`) | pass: the established call keeps its audio and the new call is refused as busy | pass |
| renewal (`engine/renewal/1001`) | pass: a registration of 30 s reports no change for 45 s and FreePBX still rings the phone | pass (UDP, TCP, TLS) |
| all | 10 scenarios, 0 failed ([report](go-m5/freepbx-report.json), [log](go-m5/freepbx.log)) | 75 scenarios, 0 failed ([report](go-m5/asterisk-report.json)) |
| the calls again on an impaired network | pass, 8 scenarios: 1 % loss, 0 to 30 ms of delay and a tenth of the packets out of order in both directions (`build/vm/impair.sh` on both VMs; each side dropped packets, the log has the counts); echo in both codecs, the call between two extensions, hold and mute, the transfer, DTMF and do-not-disturb ([report](go-m5/freepbx-degraded-report.json)) | not run impaired |

Seen against FreePBX and not changed ([runner's warnings](go-m5/freepbx-runner.log)):

- FreePBX sends a `NOTIFY` outside any dialog to an extension that registers (its message-waiting state). The softphone has no use for it and logs it as a request it could not handle.
- Twice, at the end of a scenario, the unregistration was written to a connection the scenario had already closed, and is logged as failed. The registration then lapses by itself.
- A transfer to a number that does not exist is accepted by FreePBX, which takes the other party away and then reports 503: the softphone says the transfer failed and is left in a call without audio until it is hung up. The lab's Asterisk refuses such a transfer and the call goes on (`engine/transfer/failed/1001-1002`, which is therefore not run against FreePBX).
- FreePBX names itself in `From` by its first network card's address, not the one the softphone reached it on; nothing depends on it.

## Not covered

- TCP, TLS and SRTP on FreePBX: the installation has the UDP transport only, and the scenarios took it as installed.
- The packaged application (AppImage, `.exe`) against FreePBX, and anything heard by a person: task 7.2.
- FreePBX's own firewall and intrusion detection, which the installer brings: not configured; the fence is the VM's own packet filter.
- The script's teardown path (above).

## Runs

Commit of the scenarios and scripts: the one this receipt is committed with. `make go-check` passes. `make go-freepbx` (with `KEEP=1`): exit 0, the table above. `make go-lab`: exit 0, 75 scenarios. The first runs of this task, on its own commit, had 8 scenarios on FreePBX and 68 in the lab; the scenarios added for the parity gate were run on the commit of the [parity receipt](go-m5-parity-2026-11-12.md).
