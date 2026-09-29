#!/usr/bin/env python3
"""Renders status markdown with rich, marking blocks that changed since the previous status.

Usage: render.py WIDTH STATUS_MD [PREVIOUS_MD]
"""
import re
import sys
from pathlib import Path

from rich.console import Console
from rich.markdown import Markdown
from rich.text import Text

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


def main():
    width = int(sys.argv[1])
    current = blocks(Path(sys.argv[2]).read_text())
    prev_path = Path(sys.argv[3]) if len(sys.argv) > 3 else None
    previous = blocks(prev_path.read_text()) if prev_path and prev_path.is_file() else None

    prev_keys = {key(s, t) for s, k, t in previous or [] if k != "heading"}
    # The counts cover list items; the summary sentence changes on most refreshes.
    prev_items = {key(s, t) for s, k, t in previous or [] if k == "item"}
    cur_items = {key(s, t) for s, k, t in current if k == "item"}

    console = Console(width=width, force_terminal=True, highlight=False)
    if previous is not None:
        changed = len(cur_items - prev_items)
        # A move to another section shows as a change, not a removal.
        removed = len({k.split("|", 1)[1] for k in prev_items} - {k.split("|", 1)[1] for k in cur_items})
        if changed or removed:
            console.print(Text(f"▌ {changed} new or changed · {removed} removed since last refresh", style="dim yellow"))
            console.print()

    inner = Console(width=width - 2, force_terminal=True, highlight=False)
    for i, (section, kind, text) in enumerate(current):
        if kind == "heading" and i:
            print()
        is_new = previous is not None and kind != "heading" and key(section, text) not in prev_keys
        mark = MARK if is_new else " "
        for line in render_block(inner, text):
            print(f"{mark} {line}")


if __name__ == "__main__":
    main()
