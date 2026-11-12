#!/usr/bin/env python3
"""Layout proposals for the go-microsip window, as FigCraft command scripts.

Writes one script per layout (run with `figcraft-cli run`, see build.sh) and the contrast report.
Every text, icon, control boundary and focus ring is recorded with the color it is drawn on, and
the run fails when a text pair is below 7:1 (WCAG AAA) or a non-text pair is below 3:1.

Usage: generate.py <script-dir> <contrast.md>
"""

import base64
import json
import pathlib
import sys

FONT = "Inter"  # built into FigCraft, so renders match on every machine
W, H = 360, 560

# The paths of frontend/src/components/Icon.vue, plus home, keypad and menu.
ICONS = {
    "phone": "M5 3h4l2 5-2.5 1.5a11 11 0 0 0 6 6L16 13l5 2v4a2 2 0 0 1-2 2A16 16 0 0 1 3 5a2 2 0 0 1 2-2z",
    "hangup": "M2 14a14 14 0 0 1 20 0v3l-5 1-1-4a12 12 0 0 0-8 0l-1 4-5-1z",
    "in": "M19 5 6 18M6 9v9h9",
    "out": "M5 19 18 6M9 6h9v9",
    "missed": "M4 6l8 8 8-8M4 12V6h6M4 20h16",
    "mic": "M9 3h6v9H9zM5 11a7 7 0 0 0 14 0M12 18v3",
    "micOff": "M9 3h6v9H9zM5 11a7 7 0 0 0 14 0M12 18v3M3 3l18 18",
    "pause": "M8 5v14M16 5v14",
    "lock": "M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4",
    "transfer": "M4 8h14M14 4l4 4-4 4M20 16H6M10 12l-4 4 4 4",
    "search": "M10.5 4a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13zM15.5 15.5 20 20",
    "settings": "M4 7h9M17 7h3M4 17h3M11 17h9M13 4v6h4V4zM7 14v6h4v-6z",
    "down": "M6 9l6 6 6-6",
    "close": "M6 6l12 12M18 6 6 18",
    "backspace": "M9 5h12v14H9l-6-7zM12 9l5 6M17 9l-5 6",
    "check": "M5 12l5 5 9-10",
    "ok": "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM8 12l3 3 5-6",
    "wait": "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM12 7v5l3 2",
    "dnd": "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM7.5 12h9",
    "user": "M12 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM4 21a8 8 0 0 1 16 0",
    "bell": "M6 17v-6a6 6 0 0 1 12 0v6l2 2H4zM10 22h4",
    "home": "M4 11l8-7 8 7v9h-5v-6H9v6H4z",
    "keypad": "M6 5h.01M12 5h.01M18 5h.01M6 12h.01M12 12h.01M18 12h.01M6 19h.01M12 19h.01M18 19h.01",
}

THEMES = {
    "light": dict(
        bg="#F3F5F7", panel="#FFFFFF", sunken="#E7EBEF", key="#FFFFFF",
        text="#0B1220", muted="#3D4654", border="#667080", divider="#D5DAE0",
        accent="#0A429E", on_accent="#FFFFFF", select="#DCE8FB",
        call="#0C5A2A", on_call="#FFFFFF", hangup="#8F1313", on_hangup="#FFFFFF",
        ok="#0C5A2A", ok_soft="#E1F3E6", ring="#0A429E", ring_soft="#DCE8FB",
        warn="#6A3A00", warn_soft="#FBEED6", error="#8F1313", error_soft="#FBE4E4",
        overlay="#0B122099", shadow="#0B122040",
    ),
    "dark": dict(
        bg="#0E1116", panel="#171B21", sunken="#080A0D", key="#1F242C",
        text="#F3F5F7", muted="#B9C1CC", border="#7D8796", divider="#2A3039",
        accent="#9CC2FF", on_accent="#071B3D", select="#16294A",
        call="#5FE08B", on_call="#05230F", hangup="#FF8080", on_hangup="#2A0404",
        ok="#5FE08B", ok_soft="#12301D", ring="#9CC2FF", ring_soft="#1B3157",
        warn="#FFCB6B", warn_soft="#3A2A08", error="#FF9C9C", error_soft="#401515",
        overlay="#000000A6", shadow="#00000080",
    ),
}

# Fictional data only.
ACCOUNT = "1001@pbx.invalid"
HISTORY = [
    ("out", "Ana Lima", "1002", "Answered", "35 min ago", "1:24"),
    ("missed", "Bruno Reis", "1003", "Missed", "1 h ago", ""),
    ("in", "Support desk", "1005", "Answered", "3 h ago", "3:12"),
    ("out", "Bruno Reis", "1003", "No answer", "5 h ago", ""),
    ("out", "Carla Dias", "1004", "Answered", "Yesterday", "2:08"),
    ("in", "Ana Lima", "1002", "Answered", "Yesterday", "0:48"),
    ("missed", "Davi Rocha", "1006", "Missed", "2 d ago", ""),
    ("in", "Carla Dias", "1004", "Answered", "3 d ago", "5:31"),
    ("out", "Support desk", "1005", "Busy", "3 d ago", ""),
]
DIRECTION = {"out": "Outgoing", "in": "Incoming", "missed": "Incoming"}
KEYS = [("1", ""), ("2", "ABC"), ("3", "DEF"), ("4", "GHI"), ("5", "JKL"), ("6", "MNO"),
        ("7", "PQRS"), ("8", "TUV"), ("9", "WXYZ"), ("*", ""), ("0", ""), ("#", "")]
# state -> (icon, label, color token); every state is an icon and a label, never a color alone
STATES = {
    "registered": ("ok", "Registered", "ok"),
    "connected": ("phone", "Connected", "ok"),
    "encrypted": ("lock", "Encrypted", "ok"),
    "muted": ("micOff", "Muted", "warn"),
    "incoming": ("bell", "Incoming call", "ring"),
}


def luminance(color):
    channels = [int(color[i:i + 2], 16) / 255 for i in (1, 3, 5)]
    r, g, b = [v / 12.92 if v <= 0.03928 else ((v + 0.055) / 1.055) ** 2.4 for v in channels]
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def ratio(a, b):
    hi, lo = sorted((luminance(a), luminance(b)), reverse=True)
    return (hi + 0.05) / (lo + 0.05)


def initials(name):
    return "".join(word[0] for word in name.split()[:2]).upper()


class Box:
    def __init__(self, ref, bg):
        self.ref, self.bg = ref, bg


class Doc:
    """One FigCraft script: frames with auto layout, text and icons, and the contrast pairs."""

    def __init__(self, layout, radius):
        self.layout, self.radius = layout, radius
        self.steps, self.pairs, self.count = [], [], 0
        self.t, self.theme, self.screen = None, "", ""

    def add(self, command, params, ref=None):
        step = {"command": command, "params": params}
        if ref:
            step["as"] = ref
        self.steps.append(step)

    def check(self, kind, fg, bg, what):
        self.pairs.append((self.layout, self.theme, self.screen, kind, fg, bg, what))

    def frame(self, parent, name="", w=None, h=None, fill=None, stroke=None, sides=None, r=0,
              dir="h", pad=0, gap=0, main="min", cross="min", grow=False, growv=False, at=None,
              ctl=False, focus=False, clip=False, shadow=False):
        self.count += 1
        ref = f"n{self.count}"
        props = {"fill": fill or "none", "layout": "horizontal" if dir == "h" else "vertical",
                 "padding": pad, "gap": gap, "mainAlign": main, "crossAlign": cross, "clip": clip,
                 "sizingH": "fixed" if w is not None else "fill" if grow else "hug",
                 "sizingV": "fixed" if h is not None else "fill" if growv else "hug"}
        params = {"type": "frame", "name": name or "box", "props": props}
        if w is not None:
            params["width"] = w
        if h is not None:
            params["height"] = h
        if stroke:
            props.update(stroke=stroke, strokeAlign="inside")
            if sides:  # [top, right, bottom, left] weights
                for side, weight in zip(("Top", "Right", "Bottom", "Left"), sides):
                    props[f"stroke{side}Weight"] = weight
            else:
                props["strokeWeight"] = 1
        if r:
            props["cornerRadius"] = r
        outer = parent.bg if parent else None
        effects = []
        if focus:  # a 2px ring, 2px away from the control
            effects += [{"type": "dropShadow", "color": self.t["accent"], "offset": [0, 0], "radius": 0, "spread": 4},
                        {"type": "dropShadow", "color": outer, "offset": [0, 0], "radius": 0, "spread": 2}]
            self.check("ui", self.t["accent"], outer, f"focus ring of {name}")
        if shadow:
            effects.append({"type": "dropShadow", "color": self.t["shadow"], "offset": [0, 8], "radius": 24})
        if effects:
            props["effects"] = effects
        if parent:
            params["parent"] = {"$ref": parent.ref}
            if at:
                props.update(absolute=True, x=at[0], y=at[1])
        else:
            params["x"], params["y"] = at
        if ctl:  # the boundary of a control: its border or its fill against what is around it
            edge = max((c for c in (stroke, fill) if c), key=lambda c: ratio(c, outer))
            self.check("ui", edge, outer, f"boundary of {name}")
        self.add("node.create", params, ref)
        return Box(ref, fill or outer)

    def text(self, parent, value, size=13, weight="Regular", color=None, tnum=False, grow=False,
             align=None):
        color = color or self.t["text"]
        props = {"fontFamily": FONT, "fontStyle": weight, "fontSize": size, "fill": color}
        if tnum:
            props["features"] = {"tnum": True}
        if grow:
            props.update(sizingH="fill", textAutoResize="height")
        if align:
            props["textAlign"] = align
        self.add("node.create", {"type": "text", "parent": {"$ref": parent.ref}, "text": value,
                                 "props": props})
        self.check("text", color, parent.bg, value)

    def icon(self, parent, name, size=18, color=None):
        color = color or self.t["text"]
        svg = (f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" '
               f'viewBox="0 0 24 24" fill="none" stroke="{color}" stroke-width="{3 if name == "keypad" else 2}" '
               f'stroke-linecap="round" stroke-linejoin="round"><path d="{ICONS[name]}"/></svg>')
        self.count += 1
        ref = f"n{self.count}"
        self.add("file.import", {"data": base64.b64encode(svg.encode()).decode(),
                                 "name": f"{name}.svg", "x": -2000, "y": -2000}, ref)
        self.add("node.reparent", {"ids": [{"$ref": ref}], "parent": {"$ref": parent.ref}})
        self.check("ui", color, parent.bg, f"icon {name}")

    # Widgets shared by the three layouts.

    def button(self, parent, label="", icon=None, kind="plain", w=None, h=44, grow=False, r=None,
               vertical=False, focus=False, size=14, icon_size=18):
        t = self.t
        fill, fg, stroke = {
            "plain": (t["panel"], t["text"], t["border"]),
            "on": (t["accent"], t["on_accent"], None),
            "call": (t["call"], t["on_call"], None),
            "hangup": (t["hangup"], t["on_hangup"], None),
        }[kind]
        box = self.frame(parent, label or icon, w=w, h=h, fill=fill, stroke=stroke,
                         r=self.radius if r is None else r, dir="v" if vertical else "h",
                         gap=3 if vertical else 8, main="center", cross="center", grow=grow,
                         ctl=True, focus=focus)
        if icon:
            self.icon(box, icon, icon_size, fg)
        if label:
            self.text(box, label, size, "Semi Bold", fg)
        return box

    def chip(self, parent, state, size=12):
        icon, label, token = STATES[state]
        box = self.frame(parent, label, h=size + 12, fill=self.t[token + "_soft"],
                         r=min(self.radius, (size + 12) // 2), pad=[0, 8, 0, 6], gap=4,
                         main="center", cross="center")
        self.icon(box, icon, size + 2, self.t[token])
        self.text(box, label, size, "Semi Bold", self.t[token])

    def avatar(self, parent, name, size):
        box = self.frame(parent, "avatar", w=size, h=size, fill=self.t["select"], r=size // 2,
                         main="center", cross="center")
        self.text(box, initials(name), round(size * 0.36), "Semi Bold")

    def keypad(self, parent, key_h=None, stacked=True, digit=22, gap=6, extra_row=None):
        """Twelve keys; `extra_row` draws a fifth row. Keys grow to share the height by default."""
        pad = self.frame(parent, "keypad", dir="v", gap=gap, grow=True, growv=key_h is None)
        for start in range(0, 12, 3):
            row = self.frame(pad, "row", gap=gap, grow=True, h=key_h, growv=key_h is None)
            for value, letters in KEYS[start:start + 3]:
                key = self.frame(row, f"key {value}", fill=self.t["key"], stroke=self.t["border"],
                                 r=self.radius, dir="v" if stacked else "h", gap=0 if stacked else 6,
                                 main="center", cross="center", grow=True, growv=True, ctl=True)
                self.text(key, value, digit, "Medium", tnum=True)
                if letters:
                    self.text(key, letters, 11, "Medium", self.t["muted"])
        if extra_row:
            extra_row(self.frame(pad, "row", gap=gap, grow=True, h=key_h, growv=key_h is None))
        return pad

    def history_row(self, parent, entry, h, selected=False, name_size=14, sub_size=12, icon_bg=0,
                    last=False):
        kind, name, number, result, when, duration = entry
        t = self.t
        row = self.frame(parent, f"call {name}", h=h, grow=True, fill=t["select"] if selected else None,
                         stroke=None if last else t["divider"], sides=[0, 0, 1, 0],
                         pad=[0, 10, 0, 10], gap=10, cross="center", focus=selected)
        color = t["error"] if kind == "missed" else t["text"]
        if icon_bg:
            badge = self.frame(row, "direction", w=icon_bg, h=icon_bg, r=icon_bg // 2,
                               fill=t["error_soft"] if kind == "missed" else t["select"],
                               main="center", cross="center")
            self.icon(badge, kind, 16, color)
        else:
            self.icon(row, kind, 18, color)
        middle = self.frame(row, "who", dir="v", gap=2, grow=True)
        self.text(middle, name, name_size, "Semi Bold")
        line = self.frame(middle, "what", gap=4)
        if kind == "missed":
            self.text(line, "Missed", sub_size, "Semi Bold", t["error"])
            self.check("text", t["error"], t["select"], "Missed (when its row is selected)")
            self.text(line, f"· {DIRECTION[kind]} · {number}", sub_size, color=t["muted"], tnum=True)
        else:
            self.text(line, f"{DIRECTION[kind]} · {result} · {number}", sub_size, color=t["muted"],
                      tnum=True)
        right = self.frame(row, "when", dir="v", gap=2, cross="max")
        self.text(right, when, sub_size, color=t["muted"])
        if duration:
            self.text(right, duration, sub_size, tnum=True)

    def overlay(self, win):
        self.frame(win, "dimmed window", w=W, h=H, fill=self.t["overlay"], at=(0, 0))


# Layout A: the MicroSIP arrangement, refined. Square corners (design D7 as it is today).


def a_shell(d, win, tab):
    t = d.t
    bar = d.frame(win, "tabs", h=44, grow=True, fill=t["sunken"])
    for name, icon, width in (("Dialpad", "keypad", 100), ("Calls", "wait", 82), ("Contacts", "user", 104)):
        active = name == tab
        item = d.frame(bar, f"tab {name}", w=width, h=44, fill=t["bg"] if active else None,
                       stroke=t["accent"] if active else None, sides=[3, 0, 0, 0], gap=6,
                       main="center", cross="center")
        if active:
            d.check("ui", t["accent"], t["sunken"], "selected tab marker")
        d.icon(item, icon, 16, t["text"] if active else t["muted"])
        d.text(item, name, 13, "Semi Bold" if active else "Medium", t["text"] if active else t["muted"])
    menu = d.frame(bar, "Menu", h=44, grow=True, gap=4, main="center", cross="center")
    d.text(menu, "Menu", 13, "Medium")
    d.icon(menu, "down", 14)
    body = d.frame(win, "content", dir="v", pad=10, gap=8, grow=True, growv=True)
    status = d.frame(win, "status bar", h=30, grow=True, fill=t["sunken"], stroke=t["divider"],
                     sides=[1, 0, 0, 0], pad=[0, 10, 0, 10], main="spaceBetween", cross="center")
    left = d.frame(status, "registration", gap=6, cross="center")
    d.icon(left, "ok", 15, t["ok"])
    d.text(left, "Registered", 12, "Semi Bold")
    d.text(status, ACCOUNT, 12, color=t["muted"], tnum=True)
    return body


def a_dialpad(d, win, focus=True):
    t = d.t
    body = a_shell(d, win, "Dialpad")
    display = d.frame(body, "display", h=88, grow=True, dir="v", gap=6)
    field = d.frame(display, "number", h=52, grow=True, fill=t["panel"], stroke=t["border"],
                    pad=[0, 12, 0, 12], cross="center", ctl=True, focus=focus)
    d.text(field, "1002", 22, "Medium", tnum=True, grow=True)
    match = d.frame(display, "matching contact", gap=6, cross="center", pad=[4, 0, 0, 2])
    d.icon(match, "user", 14, t["muted"])
    d.text(match, "Ana Lima", 13, "Semi Bold")
    d.text(match, "· contact", 13, color=t["muted"])
    d.keypad(body)
    edit = d.frame(body, "edit", h=40, grow=True, gap=6)
    d.button(edit, "Delete", "backspace", h=40, grow=True, size=13, icon_size=16)
    d.button(edit, "+", h=40, grow=True, size=18)
    d.button(edit, "Clear", "close", h=40, grow=True, size=13, icon_size=16)
    actions = d.frame(body, "actions", h=48, grow=True, gap=6)
    d.button(actions, "Call", "phone", "call", h=48, grow=True, size=15)
    d.button(actions, "DND", "dnd", w=96, h=48)


def a_call(d, win):
    t = d.t
    body = a_shell(d, win, "Dialpad")
    panel = d.frame(body, "call", h=88, grow=True, fill=t["panel"], stroke=t["border"], dir="v",
                    pad=[7, 12, 7, 12], gap=3)
    top = d.frame(panel, "who", grow=True, main="spaceBetween", cross="center")
    d.text(top, "Ana Lima", 17, "Semi Bold")
    d.text(top, "0:42", 17, "Medium", tnum=True)
    d.text(panel, "1002", 13, color=t["muted"], tnum=True)
    chips = d.frame(panel, "state", gap=6)
    for state in ("connected", "encrypted", "muted"):
        d.chip(chips, state)
    d.keypad(body)
    controls = d.frame(body, "controls", h=40, grow=True, gap=6)
    d.button(controls, "Unmute", "micOff", "on", h=40, grow=True, size=13, icon_size=16, focus=True)
    d.button(controls, "Hold", "pause", h=40, grow=True, size=13, icon_size=16)
    d.button(controls, "Transfer", "transfer", h=40, grow=True, size=13, icon_size=16)
    actions = d.frame(body, "actions", h=48, grow=True, gap=6)
    d.button(actions, "End call", "hangup", "hangup", h=48, grow=True, size=15)
    d.button(actions, "DND", "dnd", w=96, h=48)


def a_incoming(d, win):
    t = d.t
    a_dialpad(d, win, focus=False)
    d.overlay(win)
    dialog = d.frame(win, "Incoming call", w=320, fill=t["panel"], stroke=t["border"], dir="v",
                     at=(20, 168), shadow=True)
    title = d.frame(dialog, "title", h=40, grow=True, fill=t["sunken"], pad=[0, 12, 0, 12], gap=8,
                    cross="center")
    d.icon(title, "bell", 16, t["ring"])
    d.text(title, "Incoming call", 14, "Semi Bold")
    inner = d.frame(dialog, "body", dir="v", pad=14, gap=10, grow=True)
    who = d.frame(inner, "who", dir="v", gap=2)
    d.text(who, "Ana Lima", 22, "Semi Bold")
    d.text(who, "1002", 14, color=t["muted"], tnum=True)
    d.chip(inner, "encrypted")
    buttons = d.frame(inner, "buttons", h=48, grow=True, gap=8)
    d.button(buttons, "Answer", "phone", "call", h=48, grow=True, size=15, focus=True)
    d.button(buttons, "Reject", "hangup", "hangup", h=48, grow=True, size=15)


def a_history(d, win):
    t = d.t
    body = a_shell(d, win, "Calls")
    filters = d.frame(body, "filter", h=36, grow=True, gap=4)
    d.button(filters, "All", "check", "on", h=36, grow=True, size=13, icon_size=14)
    for name in ("Incoming", "Outgoing", "Missed"):
        d.button(filters, name, h=36, grow=True, size=13)
    rows = d.frame(body, "calls", fill=t["panel"], stroke=t["divider"], dir="v", grow=True,
                   growv=True)
    for index, entry in enumerate(HISTORY[:7]):
        d.history_row(rows, entry, 52, selected=index == 0)
    bottom = d.frame(body, "search", h=40, grow=True, gap=6)
    field = d.frame(bottom, "Search calls", h=40, grow=True, fill=t["panel"], stroke=t["border"],
                    pad=[0, 10, 0, 10], gap=8, cross="center", ctl=True)
    d.icon(field, "search", 16, t["muted"])
    d.text(field, "Search calls", 13, color=t["muted"])
    d.button(bottom, "Clear all", w=92, h=40, size=13)


# Layout B: the call and the people first, the keypad one tap away. Rounded (needs D7 changed).


def b_shell(d, win, tab=None):
    t = d.t
    header = d.frame(win, "header", h=56, grow=True, pad=[0, 12, 0, 14], main="spaceBetween",
                     cross="center")
    left = d.frame(header, "account", dir="v", gap=3)
    d.text(left, ACCOUNT, 13, "Semi Bold", tnum=True)
    status = d.frame(left, "registration", gap=4, cross="center")
    d.icon(status, "ok", 14, t["ok"])
    d.text(status, "Registered", 12, "Semi Bold", t["ok"])
    right = d.frame(header, "tools", gap=8, cross="center")
    d.button(right, "DND", "dnd", w=78, h=36, r=18, size=12, icon_size=15)
    d.button(right, icon="settings", w=36, h=36, r=18, icon_size=16)
    body = d.frame(win, "content", dir="v", grow=True, growv=True)
    if tab:
        nav = d.frame(win, "navigation", h=60, grow=True, fill=t["panel"], stroke=t["divider"],
                      sides=[1, 0, 0, 0])
        for name, icon in (("Home", "home"), ("Calls", "wait"), ("Contacts", "user"), ("Keypad", "keypad")):
            active = name == tab
            item = d.frame(nav, f"go to {name}", w=W // 4, h=59, dir="v", gap=3, main="center",
                           cross="center")
            pill = d.frame(item, "marker", w=52, h=26, r=13, fill=t["accent"] if active else None,
                           main="center", cross="center")
            if active:
                d.check("ui", t["accent"], t["panel"], "selected destination marker")
            d.icon(pill, icon, 18, t["on_accent"] if active else t["muted"])
            d.text(item, name, 11, "Semi Bold" if active else "Medium",
                   t["text"] if active else t["muted"])
    return body


def b_home(d, win, focus=True):
    t = d.t
    body = b_shell(d, win, "Home")
    inner = d.frame(body, "home", dir="v", pad=[4, 12, 12, 12], gap=12, grow=True, growv=True)
    search = d.frame(inner, "Name or number", h=48, grow=True, fill=t["panel"], stroke=t["border"],
                     r=24, pad=[0, 16, 0, 16], gap=10, cross="center", ctl=True, focus=focus)
    d.icon(search, "search", 18, t["muted"])
    d.text(search, "Name or number", 14, color=t["muted"])
    d.text(inner, "Favorites", 12, "Semi Bold", t["muted"])
    favorites = d.frame(inner, "favorites", grow=True, main="spaceBetween")
    for name in ("Ana Lima", "Bruno Reis", "Carla Dias", "Support desk"):
        tile = d.frame(favorites, f"call {name}", w=78, dir="v", gap=6, cross="center")
        d.avatar(tile, name, 48)
        d.text(tile, name.split()[0], 12, "Medium")
    d.text(inner, "Recent", 12, "Semi Bold", t["muted"])
    rows = d.frame(inner, "recent", fill=t["panel"], stroke=t["divider"], r=d.radius, dir="v",
                   grow=True, clip=True)
    for index, (kind, name, number, _, when, _) in enumerate(HISTORY[:4]):
        row = d.frame(rows, f"recent {name}", h=55, grow=True, stroke=None if index == 3 else t["divider"],
                      sides=[0, 0, 1, 0], pad=[0, 8, 0, 10], gap=10, cross="center")
        d.avatar(row, name, 36)
        middle = d.frame(row, "who", dir="v", gap=2, grow=True)
        d.text(middle, name, 14, "Semi Bold")
        line = d.frame(middle, "what", gap=4, cross="center")
        missed = kind == "missed"
        d.icon(line, kind, 13, t["error"] if missed else t["muted"])
        if missed:
            d.text(line, "Missed", 12, "Semi Bold", t["error"])
            d.text(line, f"· {when}", 12, color=t["muted"])
        else:
            d.text(line, f"{DIRECTION[kind]} · {when}", 12, color=t["muted"])
        d.button(row, icon="phone", kind="call", w=40, h=40, r=20)


def b_keypad(d, win):
    t = d.t
    body = b_shell(d, win, "Keypad")
    inner = d.frame(body, "keypad", dir="v", pad=[0, 12, 12, 12], main="spaceBetween",
                    grow=True, growv=True)
    display = d.frame(inner, "number", h=68, grow=True, cross="center")
    d.frame(display, "balance", w=40, h=40)
    middle = d.frame(display, "value", dir="v", gap=2, cross="center", grow=True)
    d.text(middle, "1002", 30, "Semi Bold", tnum=True)
    d.text(middle, "Ana Lima", 12, "Medium", t["muted"])
    d.button(display, icon="close", w=40, h=40, r=20, icon_size=16)
    grid = d.frame(inner, "keys", dir="v", gap=8, grow=True)

    def cell(row, name):
        return d.frame(row, name, w=104, h=60, main="center", cross="center")

    def round_key(row, value, letters="", icon=None):
        key = d.frame(cell(row, f"cell {value}"), f"key {value}", w=60, h=60, r=30, fill=t["key"],
                      stroke=t["border"], dir="v", main="center", cross="center", ctl=True)
        if icon:
            d.icon(key, icon, 20)
        else:
            d.text(key, value, 21, "Medium", tnum=True)
        if letters:
            d.text(key, letters, 11, "Medium", t["muted"])

    for start in range(0, 12, 3):
        row = d.frame(grid, "row", grow=True, main="spaceBetween")
        for value, letters in KEYS[start:start + 3]:
            round_key(row, value, letters)
    row = d.frame(grid, "row", grow=True, main="spaceBetween")
    round_key(row, "+")
    d.button(cell(row, "cell call"), "Call", "phone", "call", w=104, h=56, r=28, size=15, focus=True)
    round_key(row, "Delete", icon="backspace")


def b_person(d, parent, name, number, avatar, size):
    who = d.frame(parent, "who", dir="v", gap=4, cross="center")
    d.avatar(who, name, avatar)
    d.frame(who, "space", w=1, h=4)
    d.text(who, name, size, "Semi Bold")
    d.text(who, number, 15, color=d.t["muted"], tnum=True)
    return who


def b_call(d, win):
    body = b_shell(d, win)
    inner = d.frame(body, "call", dir="v", pad=[4, 16, 16, 16], main="spaceBetween", cross="center",
                    grow=True, growv=True)
    chips = d.frame(inner, "state", gap=6)
    for state in ("connected", "encrypted", "muted"):
        d.chip(chips, state)
    who = b_person(d, inner, "Ana Lima", "1002", 96, 26)
    d.text(who, "0:42", 22, "Medium", tnum=True)
    controls = d.frame(inner, "controls", grow=True, main="spaceBetween")
    for label, icon, kind in (("Unmute", "micOff", "on"), ("Hold", "pause", "plain"),
                              ("Keypad", "keypad", "plain"), ("Transfer", "transfer", "plain")):
        item = d.frame(controls, label, w=76, dir="v", gap=6, cross="center")
        d.button(item, icon=icon, kind=kind, w=60, h=60, r=30, icon_size=22, focus=kind == "on")
        d.text(item, label, 12, "Semi Bold")
    d.button(inner, "End call", "hangup", "hangup", h=56, grow=True, r=28, size=16, icon_size=20)


def b_incoming(d, win):
    t = d.t
    b_home(d, win, focus=False)
    d.overlay(win)
    sheet = d.frame(win, "Incoming call", w=W, h=336, fill=t["panel"], stroke=t["border"],
                    sides=[1, 0, 0, 0], r=[24, 24, 0, 0], dir="v", pad=[18, 16, 16, 16],
                    main="spaceBetween", cross="center", at=(0, H - 336))
    d.chip(sheet, "incoming")
    b_person(d, sheet, "Ana Lima", "1002", 72, 24)
    d.chip(sheet, "encrypted")
    buttons = d.frame(sheet, "buttons", h=56, grow=True, gap=12)
    d.button(buttons, "Reject", "hangup", "hangup", h=56, grow=True, r=28, size=16, icon_size=20)
    d.button(buttons, "Answer", "phone", "call", h=56, grow=True, r=28, size=16, icon_size=20, focus=True)


def b_history(d, win):
    t = d.t
    body = b_shell(d, win, "Calls")
    inner = d.frame(body, "calls", dir="v", pad=[4, 12, 12, 12], gap=10, grow=True, growv=True)
    filters = d.frame(inner, "filter", gap=6)
    all_calls = d.frame(filters, "All", h=36, r=18, fill=t["accent"], pad=[0, 14, 0, 10], gap=4,
                        main="center", cross="center", ctl=True, focus=True)
    d.icon(all_calls, "check", 14, t["on_accent"])
    d.text(all_calls, "All", 13, "Semi Bold", t["on_accent"])
    for name in ("Missed", "Incoming", "Outgoing"):
        pill = d.frame(filters, name, h=36, r=18, fill=t["panel"], stroke=t["border"],
                       pad=[0, 12, 0, 12], main="center", cross="center", ctl=True)
        d.text(pill, name, 13, "Medium")
    for title, entries in (("Today", HISTORY[:4]), ("Yesterday", HISTORY[4:6])):
        d.text(inner, title, 12, "Semi Bold", t["muted"])
        rows = d.frame(inner, title, fill=t["panel"], stroke=t["divider"], r=d.radius, dir="v",
                       grow=True, clip=True)
        for index, entry in enumerate(entries):
            when = entry[4] if title == "Today" else ("16:10", "09:32")[index]
            d.history_row(rows, entry[:4] + (when, entry[5]), 50, icon_bg=32,
                          last=index == len(entries) - 1)


# Layout C: dense, with a side rail; the keypad and the recent calls share one screen.


def c_shell(d, win, tab):
    t = d.t
    rail = d.frame(win, "rail", w=68, growv=True, fill=t["sunken"], stroke=t["divider"],
                   sides=[0, 1, 0, 0], dir="v", main="spaceBetween", cross="center", pad=[0, 0, 8, 0])
    top = d.frame(rail, "navigation", dir="v")
    for name, icon in (("Dial", "keypad"), ("Calls", "wait"), ("Contacts", "user"), ("Settings", "settings")):
        active = name == tab
        item = d.frame(top, f"go to {name}", w=67, h=58, fill=t["bg"] if active else None,
                       stroke=t["accent"] if active else None, sides=[0, 0, 0, 3], dir="v", gap=4,
                       main="center", cross="center")
        if active:
            d.check("ui", t["accent"], t["sunken"], "selected destination marker")
        d.icon(item, icon, 20, t["text"] if active else t["muted"])
        d.text(item, name, 11, "Semi Bold" if active else "Medium", t["text"] if active else t["muted"])
    d.button(rail, "DND", "dnd", w=54, h=48, vertical=True, size=11, icon_size=16)
    side = d.frame(win, "content", dir="v", grow=True, growv=True)
    header = d.frame(side, "header", h=40, grow=True, stroke=t["divider"], sides=[0, 0, 1, 0],
                     pad=[0, 10, 0, 10], main="spaceBetween", cross="center")
    d.text(header, ACCOUNT, 12, "Semi Bold", tnum=True)
    d.chip(header, "registered", 11)
    return d.frame(side, "body", dir="v", pad=10, gap=8, grow=True, growv=True)


def c_dial(d, win, focus=True):
    t = d.t
    body = c_shell(d, win, "Dial")
    field = d.frame(body, "number", h=44, grow=True, fill=t["panel"], stroke=t["border"], r=d.radius,
                    pad=[0, 6, 0, 10], cross="center", ctl=True, focus=focus)
    d.text(field, "1002", 18, "Medium", tnum=True, grow=True)
    d.button(field, icon="close", w=32, h=32, icon_size=14)
    d.keypad(body, key_h=44, stacked=False, digit=18, gap=4)
    actions = d.frame(body, "actions", h=44, grow=True, gap=4)
    d.button(actions, "+", w=44, h=44, size=18)
    d.button(actions, "Call", "phone", "call", h=44, grow=True, size=14)
    d.button(actions, icon="backspace", w=44, h=44)
    d.text(body, "Recent", 11, "Semi Bold", t["muted"])
    rows = d.frame(body, "recent", fill=t["panel"], stroke=t["divider"], r=d.radius, dir="v",
                   grow=True, clip=True)
    for index, (kind, name, number, _, when, _) in enumerate(HISTORY[:4]):
        row = d.frame(rows, f"recent {name}", h=38, grow=True, stroke=None if index == 3 else t["divider"],
                      sides=[0, 0, 1, 0], pad=[0, 8, 0, 8], gap=8, cross="center")
        d.icon(row, kind, 14, t["error"] if kind == "missed" else t["text"])
        d.text(row, name, 13, "Semi Bold", grow=True)
        if kind == "missed":
            d.text(row, "Missed", 12, "Semi Bold", t["error"])
        d.text(row, number, 12, color=t["muted"], tnum=True)
        d.text(row, when.replace(" ago", ""), 12, color=t["muted"])


def c_call(d, win):
    t = d.t
    body = c_shell(d, win, "Dial")
    card = d.frame(body, "call", grow=True, fill=t["panel"], stroke=t["border"], r=d.radius, dir="v",
                   pad=10, gap=6)
    top = d.frame(card, "who", grow=True, main="spaceBetween", cross="center")
    d.text(top, "Ana Lima", 16, "Semi Bold")
    d.text(top, "0:42", 16, "Medium", tnum=True)
    second = d.frame(card, "number", grow=True, main="spaceBetween", cross="center")
    d.text(second, "1002", 12, color=t["muted"], tnum=True)
    d.chip(second, "muted", 11)
    chips = d.frame(card, "state", gap=4)
    d.chip(chips, "connected", 11)
    d.chip(chips, "encrypted", 11)
    controls = d.frame(body, "controls", h=52, grow=True, gap=4)
    d.button(controls, "Unmute", "micOff", "on", h=52, grow=True, vertical=True, size=11, focus=True)
    d.button(controls, "Hold", "pause", h=52, grow=True, vertical=True, size=11)
    d.button(controls, "Transfer", "transfer", h=52, grow=True, vertical=True, size=11)
    d.keypad(body, stacked=False, digit=18, gap=4)
    d.button(body, "End call", "hangup", "hangup", h=44, grow=True, size=14)


def c_incoming(d, win):
    t = d.t
    c_dial(d, win, focus=False)
    d.overlay(win)
    dialog = d.frame(win, "Incoming call", w=336, fill=t["panel"], stroke=t["border"], r=d.radius,
                     dir="v", pad=12, gap=10, at=(12, 12), shadow=True)
    top = d.frame(dialog, "state", grow=True, main="spaceBetween", cross="center")
    d.chip(top, "incoming")
    d.chip(top, "encrypted")
    who = d.frame(dialog, "who", dir="v", gap=2)
    d.text(who, "Ana Lima", 20, "Semi Bold")
    d.text(who, "1002", 13, color=t["muted"], tnum=True)
    buttons = d.frame(dialog, "buttons", h=44, grow=True, gap=8)
    d.button(buttons, "Answer", "phone", "call", h=44, grow=True, focus=True)
    d.button(buttons, "Reject", "hangup", "hangup", h=44, grow=True)


def c_history(d, win):
    t = d.t
    body = c_shell(d, win, "Calls")
    filters = d.frame(body, "filter", h=32, grow=True, gap=4)
    d.button(filters, "All", "check", "on", h=32, grow=True, size=12, icon_size=13)
    for name in ("In", "Out", "Missed"):
        d.button(filters, name, h=32, grow=True, size=12)
    field = d.frame(body, "Search calls", h=36, grow=True, fill=t["panel"], stroke=t["border"],
                    r=d.radius, pad=[0, 10, 0, 10], gap=8, cross="center", ctl=True)
    d.icon(field, "search", 15, t["muted"])
    d.text(field, "Search calls", 13, color=t["muted"])
    rows = d.frame(body, "calls", fill=t["panel"], stroke=t["divider"], r=d.radius, dir="v",
                   grow=True, growv=True, clip=True)
    for index, entry in enumerate(HISTORY):
        d.history_row(rows, entry, 44, selected=index == 0, name_size=13, sub_size=11,
                      last=index == len(HISTORY) - 1)


LAYOUTS = [
    ("a-classic", 0, "v", [("dialpad", a_dialpad), ("call", a_call), ("incoming", a_incoming),
                           ("history", a_history)]),
    ("b-call-first", 14, "v", [("home", b_home), ("keypad", b_keypad), ("call", b_call),
                               ("incoming", b_incoming), ("history", b_history)]),
    ("c-rail", 6, "h", [("dialpad", c_dial), ("call", c_call), ("incoming", c_incoming),
                        ("history", c_history)]),
]


def report(pairs):
    lines = ["# Contrast of the layout proposals", "",
             "Generated by `generate.py`: every text, icon, control boundary and focus ring of every",
             "screen, with the exact color it is drawn on. Text must reach 7:1, non-text 3:1.", "",
             "| Layout | Skin | Lowest text ratio | Lowest non-text ratio | Pairs checked |",
             "| --- | --- | --- | --- | --- |"]
    failures = []
    for layout, _, _, _ in LAYOUTS:
        for theme in THEMES:
            mine = [p for p in pairs if p[0] == layout and p[1] == theme]
            lowest = {}
            for kind in ("text", "ui"):
                lowest[kind] = min((ratio(p[4], p[5]), p[6], p[4], p[5]) for p in mine if p[3] == kind)
            lines.append(f"| {layout} | {theme} | {lowest['text'][0]:.2f} (`{lowest['text'][1]}`) | "
                         f"{lowest['ui'][0]:.2f} ({lowest['ui'][1]}) | {len(mine)} |")
    for kind, title, floor in (("text", "Text", 7.0), ("ui", "Non-text", 3.0)):
        lines += ["", f"## {title} pairs (at least {floor:g}:1)", ""]
        for theme in THEMES:
            lines += [f"### {theme}", "", "| Foreground | Background | Ratio | Example |",
                      "| --- | --- | --- | --- |"]
            seen = {}
            for p in pairs:
                if p[1] == theme and p[3] == kind:
                    seen.setdefault((p[4], p[5]), p[6])
            for (fg, bg), what in sorted(seen.items(), key=lambda item: ratio(*item[0])):
                value = ratio(fg, bg)
                lines.append(f"| `{fg}` | `{bg}` | {value:.2f} | {what} |")
                if value < floor:
                    failures.append(f"{theme}: {fg} on {bg} is {value:.2f}, below {floor:g} ({what})")
            lines.append("")
    return "\n".join(lines).rstrip() + "\n", failures


def main(script_dir, report_path):
    pairs = []
    for layout, radius, direction, screens in LAYOUTS:
        doc = Doc(layout, radius)
        for row, theme in enumerate(THEMES):
            doc.t, doc.theme = THEMES[theme], theme
            for column, (screen, draw) in enumerate(screens):
                doc.screen = screen
                win = doc.frame(None, f"{screen}-{theme}", w=W, h=H, fill=doc.t["bg"], dir=direction,
                                clip=True, at=(column * (W + 40), row * (H + 40)))
                draw(doc, win)
        pathlib.Path(script_dir, f"{layout}.json").write_text(json.dumps(doc.steps))
        pairs += doc.pairs
    text, failures = report(pairs)
    pathlib.Path(report_path).write_text(text)
    if failures:
        sys.exit("contrast below the floor:\n  " + "\n  ".join(sorted(set(failures))))


if __name__ == "__main__":
    main(*sys.argv[1:3])
