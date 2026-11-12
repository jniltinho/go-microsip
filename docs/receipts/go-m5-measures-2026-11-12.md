# Go migration, M5: start-up, memory and size beside the Rust candidate (task 7.3)

Change `openspec/changes/migrate-to-go-wails`, task 7.3. The Rust candidate's receipts hold no start-up or memory numbers to measure against: its own task for them (9.7 of `create-native-rust-linux-release`) was never run. Both applications were therefore measured here, the same way, on the same machines, one after the other: go-microsip `0.3.0-m3` of commit `cf3e8ef` and a local build of the Rust candidate `0.1.0-rc.3`.

These are numbers from virtual machines with two processors and no graphics hardware (the kept VMs of `build/vm`: Ubuntu 26.04 and Debian 13 with GNOME on Wayland, 4 GB; Windows 11, 6 GB). They compare the two applications with each other; they are not what either does on a person's computer.

## How

`build/vm/measure.sh` (Linux) and `build/vm/measure.ps1` (Windows), run by `make go-measure`. Each start has new, empty folders for settings and data and no account; one start with the file cache dropped (Linux) or simply the first (Windows), then ten more. Memory is taken 20 s after the last start, at rest on the home screen.

Moments timed from the launch:

- **Linux, an answered `status`**: the application's own command line asks its control socket every few hundredths of a second. The Rust candidate answers from its window's loop, so for it this is the window running. go-microsip answers before its window is drawn, so for go-microsip the next moment is the one to read.
- **The first drawn page** (go-microsip only): its window reports what it shows; the Rust candidate has no such report.
- **Windows, a window**: the process has a main window. For the Rust candidate that is before anything is drawn in it.

Memory on Linux is the proportional set size of every process of the application's session (go-microsip's web view runs in processes of its own) and the resident set of the main process; on Windows the working set and the private bytes of the process and of every process it started.

## Start-up

| Machine | Moment | go-microsip | Rust candidate |
| --- | --- | --- | --- |
| Ubuntu 26.04 | answered `status`, median of 10 (least to most) | 138 ms (123 to 160) | 124 ms (116 to 134) |
| Ubuntu 26.04 | first drawn page, median of 10 | 755 ms (740 to 768) | not reported by it |
| Ubuntu 26.04 | first start, file cache dropped | 986 ms to the drawn page | 236 ms to an answered `status` |
| Debian 13 | answered `status`, median of 10 | 162 ms (149 to 167) | 82 ms (80 to 105) |
| Debian 13 | first drawn page, median of 10 | 458 ms (446 to 474) | not reported by it |
| Debian 13 | first start, file cache dropped | 837 ms to the drawn page | 209 ms to an answered `status` |
| Windows 11 | a window, median of 10 | 603 ms (546 to 678) | 44 ms (38 to 47) |
| Windows 11 | first drawn page, median of 10 | 603 ms (560 to 668) | not reported by it |
| Windows 11 | first start | 935 ms to a window | 926 ms to a window |

## Memory, 20 s after the start, at rest

| Machine | go-microsip | Rust candidate |
| --- | --- | --- |
| Ubuntu 26.04 | 347 MB proportional in 9 processes; 281 MB resident in the main one | 75 MB proportional in 2 processes; 138 MB resident in the main one |
| Debian 13 | 382 MB proportional in 9 processes; 267 MB resident in the main one | 75 MB proportional in 2 processes; 124 MB resident in the main one |
| Windows 11 | 321 MB working set and 156 MB private in 7 processes | 52 MB working set and 273 MB private in 1 process |

## Size

| Artifact | go-microsip `0.3.0-m3` | Rust candidate `0.1.0-rc.3` (local build) |
| --- | --- | --- |
| AppImage | 11,741,688 bytes | 12,581,368 bytes |
| Linux binary | 28,250,375 bytes | 27,030,312 bytes |
| Windows `.exe` | 28,792,832 bytes | 19,078,656 bytes |
| Windows `.zip` | 11,849,736 bytes | 17,531,063 bytes |
| Windows installer | none | 18,936,320 bytes (per-user MSI) |

The Rust AppImage carries what it needs but the C library and the sound library; go-microsip's uses the desktop's GTK 4 and WebKitGTK 6.0, which are not in these numbers (design D11).

## Regressions, accepted by the owner

Each is the cost of the window being a web page in the system's web view (design D5). The owner accepted them on 2026-11-12 (decision D14 of [the parity decisions](../migration/parity-decisions.md); no installer is decision P5).

1. **A usable window takes longer.** On Linux the first drawn page comes 0.46 to 0.77 s after the launch, where the Rust candidate's window loop answers after 0.08 to 0.13 s; with a cold file cache, 0.84 to 0.99 s against 0.21 to 0.24 s. On Windows the window is there after 0.6 s against 0.04 s (an empty window, for the Rust candidate).
2. **It holds four to five times the memory on Linux**: 347 to 382 MB against 75 MB, proportional. On Windows the working set is six times the Rust candidate's (321 MB against 52 MB), and the private bytes are fewer (156 MB against 273 MB).
3. **The Windows `.exe` is half as large again** (28.8 MB against 19.1 MB) and the Linux binary slightly larger; the AppImage and the Windows `.zip` are smaller.
4. **No Windows installer** (a difference of packaging, listed with the Windows rows of the parity matrix).

## Not measured

- Start-up and memory on a computer with graphics hardware, during a call, or over hours: the manual checklist of task 7.2 uses the application on real machines.
- The published `0.1.0-rc.3` artifacts: the Rust candidate here is a local build of that version.

## Runs

`make go-measure` on the kept VMs: [Ubuntu 26.04](go-m5/measure-ubuntu2604.log), [Debian 13](go-m5/measure-debian13.log), [Windows 11](go-m5/measure-windows11.log).
