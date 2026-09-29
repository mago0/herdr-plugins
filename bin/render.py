#!/usr/bin/env python3
"""Renders status markdown with rich, marking blocks that changed since the previous status.

Usage: render.py WIDTH STATUS_MD [PREVIOUS_MD] [--max-rows N] [--count]
"""
import argparse
import re
from pathlib import Path

from rich.console import Console
from rich.markdown import Markdown

HEADING = re.compile(r"^#{1,6}\s")
ITEM = re.compile(r"^(?:[-*+]|\d+[.)])\s")


def blocks(md):
    """Splits markdown into (section, kind, text): headings, paragraphs, and top-level list items with their nested lines."""
    out, section, cur = [], "", None

    def flush():
        nonlocal cur
        if cur:
            out.append((cur[0], cur[1], "\n".join(cur[2])))
            cur = None

    for line in md.splitlines():
        if HEADING.match(line):
            flush()
            section = line.lstrip("#").strip()
            out.append((section, "heading", line))
        elif ITEM.match(line):
            flush()
            cur = [section, "item", [line]]
        elif not line.strip():
            flush()
        elif cur:
            cur[2].append(line)
        else:
            cur = [section, "para", [line]]
    flush()
    return out


MARK = "\x1b[1;33m▌\x1b[0m"
DIM = "\x1b[2m"
DIM_YELLOW = "\x1b[2;33m"
RESET = "\x1b[0m"
ANSI = re.compile(r"\x1b\[[0-9;]*m")


def render_block(console, text):
    """Renders one markdown block and trims the blank lines rich puts around it."""
    with console.capture() as cap:
        console.print(Markdown(text))
    lines = cap.get().rstrip("\n").split("\n")
    while lines and not ANSI.sub("", lines[0]).strip():
        lines.pop(0)
    while lines and not ANSI.sub("", lines[-1]).strip():
        lines.pop()
    return lines


def key(section, text):
    # Markup and spacing differences don't count as a change; a move to another section does.
    return section.lower() + "|" + re.sub(r"[\W_]+", " ", text).lower().strip()


# Sections the viewer hides first, oldest items first, when the status doesn't fit.
TRIM_ORDER = ("done", "next")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("width", type=int)
    ap.add_argument("status")
    ap.add_argument("previous", nargs="?")
    ap.add_argument("--max-rows", type=int, help="hide low-priority items beyond this many lines")
    ap.add_argument("--count", action="store_true", help="print the untrimmed line count and exit")
    args = ap.parse_args()

    current = blocks(Path(args.status).read_text())
    prev_path = Path(args.previous) if args.previous else None
    previous = blocks(prev_path.read_text()) if prev_path and prev_path.is_file() else None

    prev_keys = {key(s, t) for s, k, t in previous or [] if k != "heading"}
    # The counts cover list items; the summary sentence changes on most refreshes.
    prev_items = {key(s, t) for s, k, t in previous or [] if k == "item"}
    cur_items = {key(s, t) for s, k, t in current if k == "item"}

    head = []
    if previous is not None:
        changed = len(cur_items - prev_items)
        # A move to another section shows as a change, not a removal.
        removed = len({k.split("|", 1)[1] for k in prev_items} - {k.split("|", 1)[1] for k in cur_items})
        if changed or removed:
            head = [f"{DIM_YELLOW}▌ {changed} new or changed · {removed} removed since last refresh{RESET}", ""]

    inner = Console(width=args.width - 2, force_terminal=True, highlight=False)
    units = []
    for i, (section, kind, text) in enumerate(current):
        is_new = previous is not None and kind != "heading" and key(section, text) not in prev_keys
        mark = MARK if is_new else " "
        lines = [f"{mark} {line}" for line in render_block(inner, text)]
        if kind == "heading" and i:
            lines.insert(0, "")
        units.append({"section": section.lower(), "kind": kind, "lines": lines})

    total = len(head) + sum(len(u["lines"]) for u in units)
    if args.count:
        print(total)
        return

    hidden = 0
    if args.max_rows and total > args.max_rows:
        budget = args.max_rows - 1  # room for the hidden-items line
        for section in TRIM_ORDER:
            items = [u for u in units if u["section"] == section and u["kind"] == "item"]
            for u in reversed(items):
                if total <= budget:
                    break
                units.remove(u)
                total -= len(u["lines"])
                hidden += 1
            if items and not any(u["section"] == section and u["kind"] == "item" for u in units):
                for u in [u for u in units if u["section"] == section]:
                    units.remove(u)
                    total -= len(u["lines"])

    for line in head + [line for u in units for line in u["lines"]]:
        print(line)
    if hidden:
        print(f"{DIM}  … {hidden} older items hidden · a shows all{RESET}")

if __name__ == "__main__":
    main()
