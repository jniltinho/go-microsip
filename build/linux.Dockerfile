# The one build image of go-microsip: every `make go-*` target runs in it, so the host needs only
# Docker and make. Debian 13 is the older of the two supported hosts (Debian 13, Ubuntu 26.04+), so a
# binary linked here against its glibc, GTK 4 and WebKitGTK 6.0 runs on both.
# The build context is this folder alone: nothing of the checkout is sent to the daemon.
FROM debian:13@sha256:9c6e381421d0f48d1b8133e573f33c185ee16cbf505eda9c4a8a4791b25ea8a6
ENV DEBIAN_FRONTEND=noninteractive
RUN echo 'Acquire::Retries "5";' > /etc/apt/apt.conf.d/80-retries
# The Wails window: GTK 4 and WebKitGTK 6.0 headers (cgo). Checks: Xvfb and a session bus for the
# window, a window manager and ImageMagick for screenshots, xdotool for the keys of a call placed
# from the window, PipeWire with its PulseAudio server for the audio adapter tests. Packaging: zip,
# desktop-file-utils and appimagetool.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git zip unzip xz-utils file build-essential pkg-config \
      libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev \
      xvfb dbus dbus-x11 openbox imagemagick xdotool desktop-file-utils \
      pipewire pipewire-pulse wireplumber pulseaudio-utils \
    && rm -rf /var/lib/apt/lists/*
ARG APPIMAGETOOL_VERSION=1.9.1
ARG APPIMAGE_RUNTIME_VERSION=20251108
RUN curl -fsSL -o /usr/local/bin/appimagetool \
      https://github.com/AppImage/appimagetool/releases/download/${APPIMAGETOOL_VERSION}/appimagetool-x86_64.AppImage \
    && curl -fsSL -o /usr/local/share/appimage-runtime-x86_64 \
      https://github.com/AppImage/type2-runtime/releases/download/${APPIMAGE_RUNTIME_VERSION}/runtime-x86_64 \
    && chmod +x /usr/local/bin/appimagetool
ARG GO_VERSION=1.27.3
RUN curl -fsSL https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz | tar -C /usr/local -xz
# Node builds the frontend (Vite Plus).
ARG NODE_VERSION=24.11.1
RUN curl -fsSL https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz \
      | tar -C /usr/local --strip-components=1 -xJ
# No FUSE in containers: appimagetool runs extracted. Module mode is forced because the frozen Rust
# workspace keeps a vendor/ folder at the repository root, which would switch Go to vendor mode.
ENV PATH=/usr/local/go/bin:$PATH APPIMAGE_EXTRACT_AND_RUN=1 GOFLAGS=-mod=readonly
