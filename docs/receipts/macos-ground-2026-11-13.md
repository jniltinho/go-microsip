# macOS: the ground and a first bundle (2026-11-13)

Change `add-macos-universal`, tasks 1.4, 1.5, 2.1 and 2.2. Commit `a3e2eaf`, a clean tree, built in the image `go-microsip-build:13`. The Mac is the owner's macOS VM: macOS 15.8.1, x86_64, reached over SSH with a key; its address, user name and host key are in no file of the repository and in none of the outputs below. The host key came from the record of the application that runs the VM, which the owner made; it is the key the VM showed.

What this is evidence for: the macOS files build on Linux, the bundle is well formed, and on macOS 15 on x86_64 the commands and the window work. It is not evidence for sound, the keychain, agent control, notifications or links, none of which exists on macOS yet, nor for the arm64 slice running, which this Mac cannot do.

## The files

`make go-macos`, with nothing but Docker used for the build:

```
d7eab213e9c9a785b26bcd120a2e1e8f7d385d7a7c59ad8308c6ce297f640424  go-microsip-0.2.0-rc.1-macos-universal.zip
04a54e0f3d1ad1dccf7a4c923f37b847ff874700e34907c53682e769ad6d9280  macos/go-microsip-darwin-amd64
f28b776ccd4edf3978db310f256a304c5b057f9597b551c6d4ef705faeb54c0c  macos/go-microsip-darwin-arm64
```

The notices inside the zip are those of the Linux and Windows dependency graphs: the macOS graphs are task 6.2, and the zip is not published.

## The packaging program (2.1)

`go test ./build/macos/package`, part of `make test` (which passed, as did `make check`): the universal executable holds both slices unchanged, each on a page boundary; nine refusals (a file that is not Mach-O, one slice twice, one slice alone, three slices, an object file, an executable for another platform, one that names no minimum system, another commit, another version); the bundle holds exactly its six files, the executable with mode 755, and an information file with the identifier, the two link schemes, the reasons for the microphone and the local network, three-number versions, and the minimum system of the slice that needs the later one; an icon that is not a square of a known size is refused; a zip that does not read back as written is refused.

## The check of the zip from outside (2.2)

`build/qa/macos.sh` on the built zip:

```
macos: the bundle's six files, a universal program whose slices are go-microsip-darwin-amd64 and go-microsip-darwin-arm64, and the declarations, for macOS 13.0 and later
```

Tried against broken zips made by hand, each refused for its own reason: an extra file ("it holds: ..."), the x86_64 executable alone in place of the universal one ("the program is not a universal executable"), the x86_64 slice given as the arm64 one ("the slice ... is not ..., byte for byte"), no reason for the microphone, a declared minimum of 12.0 ("below what a slice needs"). The same files zipped again pass.

## On the Mac (1.5, 2.2)

`make go-mac`, the zip:

```
file: d7eab213e9c9a785b26bcd120a2e1e8f7d385d7a7c59ad8308c6ce297f640424  go-microsip-0.2.0-rc.1-macos-universal.zip
mac: macOS 15.8.1, x86_64
pass unpack
pass version: go-microsip 0.2.0-rc.1 (a3e2eaf4176f)
pass headless (version, doctor, MCP handshake, password-argument refusal, configuration)
pass self-test (the window opens and the page calls the backend)
mac: all checks passed
```

`make go-mac FILE=dist/go/macos/go-microsip-darwin-amd64`, the x86_64 slice alone: the same five lines pass, with the slice's checksum.

The program was run from inside the unpacked bundle, by its path, over SSH. It was not opened with `open` in a desktop session: nobody is logged in at that Mac's console (task 1.3).

With Apple's own tools on that Mac, on the same zip: `lipo -archs` says `x86_64 arm64`; `lipo -thin` gives back two files with the checksums of the two slices above; `codesign --verify` accepts the arm64 slice, whose signature is `adhoc, linker-signed`, so the joining left it intact; `plutil -lint` accepts the information file, which gives 13.0 as the minimum system and `sip` and `tel` as schemes. The run left nothing in the user's folders.

## The check itself (1.4)

- `build/qa/headless.sh` without `timeout(1)`: it passes on the Linux executable and on the AppImage of this commit in the build image, and on the Mac as above.
- `sh build/vm/mac-selftest.sh`, with stand-ins for `ssh` and `scp` that print a fictional address, user name, host key and paths of the Mac's user: after a success, an unknown host key, a Mac that wants a password, answers that are not what was asked, a failing check, a failed copy and an interrupted run, none of them is in what the run printed or in any file it left, and no temporary file remains; a missing variable stops the run and is named; five malformed targets and a host-key file inside the repository are refused. The stand-in refuses to run without the options that keep `ssh` from asking and from trusting a new key.
- The controls fail when the script is broken, tried four ways on a copy: printing what `ssh` said on a failure, dropping the option that forbids questions, keeping the temporary folder, printing the Mac's answer when it is not a version line.
- Against the real Mac: with an empty host-key file, and with a user name the key does not open (the Mac then offers a password), the run prints `FAIL connect: no connection, a host key that is not in the file, or no accepted key` and stops at once, asking nothing.
- The outputs of the four real runs and the retained logs under `artifacts/go-mac/` were searched for the Mac's address, port, user name and key material: none.

## Not done here

- Task 1.3: the Mac has a sound output and no input, and no logged-in desktop session; both are needed from task 2.3 on and are the owner's to provide.
- The arm64 slice was not run: this Mac is x86_64.
