#!/usr/bin/env python3
"""Renders status markdown with rich, marking blocks that changed since the previous status.

Usage: render.py WIDTH STATUS_MD [PREVIOUS_MD] [--max-rows N] [--count]
"""
import argparse
import re
from difflib import SequenceMatcher
from pathlib import Path

from rich.console import Console
from rich.markdown import Markdown

HEADING = re.compile(r"^#{1,6}\s")
# The viewer shows the Summary line in its header, so the body skips it.
SUMMARY = re.compile(r"^\W*summary\W*:", re.IGNORECASE)
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
        if SUMMARY.match(line) and not section:
            flush()
        elif HEADING.match(line):
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


# Two blocks with at least this share of words in common, in order, count as the same item.
SIMILAR = 0.8
# Words that carry state; a change in any of them is a real change however similar the rest is.
STATE_WORDS = {
    "approved", "merged", "closed", "open", "opened", "reopened", "failed", "failing", "passed",
    "passing", "blocked", "unblocked", "done", "deployed", "reverted", "running", "queued",
    "waiting", "ready", "pending", "cancelled", "canceled", "green", "red", "not", "no",
}


def words(text):
    return re.sub(r"[\W_]+", " ", text).lower().split()


def facts(ws):
    """Numbers, ids (repo#42 -> 42, SHAs) and state words, which must match exactly."""
    return sorted(w for w in ws if w in STATE_WORDS or any(c.isdigit() for c in w))


def same(a, b):
    if a == b:
        return True
    if facts(a) != facts(b):
        return False
    return SequenceMatcher(None, a, b, autojunk=False).ratio() >= SIMILAR


def matched(block, others, any_section=False):
    """True when some block in others is the same item: same section (unless any_section) and similar words."""
    section, ws = block
    return any((any_section or s == section) and same(ws, ows) for s, ows in others)


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

    def comparable(bs, kinds):
        return [(s.lower(), words(t)) for s, k, t in bs or [] if k in kinds]

    prev_blocks = comparable(previous, ("item", "para"))
    # The counts cover list items outside Last, which changes on most refreshes.
    prev_items = [b for b in comparable(previous, ("item",)) if b[0] != "last"]
    cur_items = [b for b in comparable(current, ("item",)) if b[0] != "last"]

    head = []
    if previous is not None:
        changed = sum(not matched(b, prev_items) for b in cur_items)
        # A move to another section shows as a change, not a removal.
        removed = sum(not matched(b, cur_items, any_section=True) for b in prev_items)
        if changed or removed:
            head = [f"{DIM_YELLOW}▌ {changed} new or changed · {removed} removed since last refresh{RESET}", ""]

    inner = Console(width=args.width - 2, force_terminal=True, highlight=False)
    units = []
    for i, (section, kind, text) in enumerate(current):
        is_new = previous is not None and kind != "heading" and not matched((section.lower(), words(text)), prev_blocks)
        mark = MARK if is_new else " "
        # Prose under a heading renders as a bullet so every section lines up.
        if kind == "para" and section:
            text = "- " + text
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
