# The files of the release, checked before the tag (task 7.7), 2026-11-13

What CI built from commit `12a1786` of `main`, which the release workflow would publish for a tag on it, and what was run on those exact files. No tag was pushed and nothing was published when this was written, and the last section says why this commit should not be the tagged one.

## The files

Built by CI, not on the developer's machine: run 43412189111 of `ci.yml`, the push of `12a1786` to `main`, all three jobs green (the publication scan with its controls; the checks, tests with the race detector, the frontend's checks, the artifacts, the headless checks, the window's self-test and the AppImage's contents, and the scan of the files; the Windows smoke of the `.exe` and the `.zip`). The release workflow publishes the artifact `dist` of that run and builds nothing.

Downloaded with `gh run download 43412189111 --name dist`; `sha256sum --check --strict SHA256SUMS` passes, and the manifest says version `0.2.0-rc.1`, commit `12a1786302cc`, a clean checkout, the purego window and the image `go-microsip-build:13`.

```
8599708d4245bea6d44ce40a1a0c5ebf62b493c30d2086d7cbf8e7e10d2ffc72  go-microsip-0.2.0-rc.1-linux-amd64
db583562b62a1595a8c0fd7d819a9dccc5dd297b848ce0c83342485a977ef19e  go-microsip-0.2.0-rc.1-x86_64.AppImage
a5375a8cac733e9a810834d4a23169023835d8471d5d2f27a17fac14f0260531  go-microsip-0.2.0-rc.1-windows-amd64.exe
5db7ad0182ad805dbc1fd19e70dffe7732277fc7faa476e8298edfd4333c2d52  go-microsip-0.2.0-rc.1-windows-amd64.zip
e3fc7c29dff51b72a0ebaf2660c72796dec32db4793123fe0e50beb23b14ba19  THIRD_PARTY_NOTICES.txt
d60fecee4784cb881715b83122b2aa0aead3e850a4ab64db7ae3d1db4675039f  sbom.cdx.json
04e4ca387c6d93e1fdc2ee8acf494b0db8b82312558aaf19c2d939aa8daf554a  build-manifest.json
```

A release whose `SHA256SUMS` differs from these lines was not checked here.

## The VM checks on those files

`KEEP=1 SECONDS=60 make go-vm GO_VM_LAB=1`, twice, with these files in `dist/go` (the test programs the VMs also run were built from the tree; the softphone was not). The VMs are the ones kept up since 2026-11-12; the host was under heavy load throughout from a program of the editor (22 of its 24 processors), which this session could not stop. The logs are in [go-m5/](go-m5/), `release-vm-*-run1.log` and `-run2.log`.

| VM | Start-up, the window, the skins, the keyring, notifications, the tray, links, the command line, MCP | Calls against the lab PBX | What failed |
| --- | --- | --- | --- |
| Ubuntu 26.04 | pass in both runs | not part of this VM's checks | both runs: after go-microsip leaves, the Rust candidate's client asks its own window for its status and times out. Run 2 also missed the 60 s sound threshold of the VM's virtual card (89 of 5996 pieces missing on the speaker, a gap of 124 ms); run 1 was within it |
| Debian 13 | pass in both runs | not part of this VM's checks | both runs: the same question to the Rust candidate's window gets no answer at the first try and `status:ok` at the second, which the log shows |
| Windows 11 | run 2: pass. Run 1: the steps that press a key failed because a window of Windows' own shell (ShellExperienceHost) was in front of the softphone | pass in both runs: registered over UDP and over TCP, called the PBX, was called by it, answered, hung up; a call given up leaves one "Missed call" notification | both runs: the sound threshold on the VM's emulated sound card, as in every run on this VM |

What this shows and what it does not:

- The start-up and the lab calls, which are what task 7.7 and the requirement "Released artifacts validated after cutover" name, pass on the exact files in every run.
- No run of a VM was clean. The step that failed on both Linux VMs is a question to the Rust candidate's client, not a function of go-microsip; it passed on Debian the night before and is slower today, on VMs that have been up for two days under a loaded host. That explanation was not proven: nobody ran it on an idle host.
- The Windows steps that failed in run 1 passed in run 2. What puts a shell window in front is still unknown; it happened in three of five runs since those steps exist.
- The Ubuntu VM's sound measurement passed in one run and missed in the other. Sound on a real sound card was not measured on any system.
- No call was made on a production PBX with these files.

## What happened next

The owner chose the pre-release `v0.2.0-rc.1` and had the VMs shut down and removed, in that order, before the run could be repeated. All five (`ubuntu2604`, `debian13`, `windows11`, `pbx`, `freepbx`) were destroyed with `vagrant destroy -f`. So:

- The tagged commit is `f342d2c`, later than `12a1786`. Between the two only documents changed (the README, the guide under `docs/`, the notes, this receipt, the task list and the archive's README): no file that goes into the executables.
- The files of the tagged commit were checked by CI alone. They were not run on a VM, and task 7.7, which asks for that, stays open. The release's notes say this.

## The files of the tag

Tag `v0.2.0-rc.1` on `f342d2c`, pushed on 2026-11-13. CI's run for that commit on `main` is 43417276337, all three jobs green. Its artifact `dist`, which the release workflow publishes, was downloaded before the tag: `sha256sum --check --strict SHA256SUMS` passes and the manifest says version `0.2.0-rc.1`, commit `f342d2ca6853`, a clean checkout.

```
4e198bce0b611f5f3bfeec6fc4fff1f9b6c94f04ccfe23f1c2c03beb702f342c  go-microsip-0.2.0-rc.1-linux-amd64
11efe67de92b4480fbfc700a39dfce709afbddc835899b46efdc80c3dc502d0f  go-microsip-0.2.0-rc.1-x86_64.AppImage
a4be5eda9fcfe3806bd2a9823fb1fb2d03e4f7296aa329f810dc0af0321c17c3  go-microsip-0.2.0-rc.1-windows-amd64.exe
21761c03997ac36892cced14e7b985b8efafee30daced6c65007aa19c9d1df58  go-microsip-0.2.0-rc.1-windows-amd64.zip
e3fc7c29dff51b72a0ebaf2660c72796dec32db4793123fe0e50beb23b14ba19  THIRD_PARTY_NOTICES.txt
d60fecee4784cb881715b83122b2aa0aead3e850a4ab64db7ae3d1db4675039f  sbom.cdx.json
894fe3b495aec41f43e29fd21c667697ff8c37cd276117aab7b55f7e65e5a22a  build-manifest.json
```

The notices and the bill of materials are the same files as those of `12a1786`; the executables differ because they carry their commit.

The release workflow (run 43418385272) passed its check of the `release` environment and waits for the owner's approval. The comparison of the published assets with these lines (task 7.10) is added once it has run.
