#!/usr/bin/env python3
"""Check that every in-document link points at a heading that exists.

Written after three broken anchors turned up in CHALLENGES.md at once, one of
them pre-existing: defect 49 cited `pm grant` as [25] -- which is the dump
backend's idle problem, pm grant being 26 -- with a slug matching no heading
at all. Anchors get written from memory, the text around them reads fine, and
nothing renders differently until somebody clicks.

Only intra-document links (`](#anchor)`) are checked. Cross-file and external
links are a different job needing a different tool, and pretending otherwise
would be the incompleteness this repository keeps recording.

    python3 docs/checks/doc-links.py $(git ls-files '*.md')
"""
import re
import sys


def slug(heading):
    """GitHub's anchor rule, near enough: drop markup, lowercase, hyphenate."""
    h = re.sub(r"`", "", heading.strip())
    h = re.sub(r"\*\*|\*|_", "", h)
    h = h.lower()
    h = re.sub(r"[^\w\s-]", "", h)
    return re.sub(r"\s+", "-", h).strip("-")


def strip_code(text):
    """Blank out fenced blocks and inline spans, keeping line numbers intact.

    Syntax quoted as an example is not a link. The very first run of this
    check flagged a quoted `](#anchor)` example, which is documentation of the
    thing being checked rather than a broken link to it.
    """
    def blank(m):
        return re.sub(r"[^\n]", " ", m.group(0))

    text = re.sub(r"```.*?```", blank, text, flags=re.S)
    text = re.sub(r"`[^`\n]*`", blank, text)
    return text


def main(paths):
    if not paths:
        # The same refusal american-spelling.py makes, for the same reason:
        # with no files this would check nothing and print a clean result,
        # which is indistinguishable from a repository with no broken links.
        print("usage: doc-links.py <file.md>...\n"
              "  sweep everything tracked:\n"
              "    python3 docs/checks/doc-links.py $(git ls-files '*.md')",
              file=sys.stderr)
        return 2

    broken = 0
    for path in paths:
        try:
            text = open(path, encoding="utf-8").read()
        except OSError:
            continue
        anchors = {slug(m.group(1))
                   for m in re.finditer(r"^#{1,6}\s+(.*)$", text, re.M)}
        text = strip_code(text)
        for m in re.finditer(r"\]\(#([^)]+)\)", text):
            if m.group(1) not in anchors:
                line = text.count("\n", 0, m.start()) + 1
                print(f"{path}:{line}: #{m.group(1)} matches no heading")
                broken += 1

    print(f"\n{len(paths)} files, {broken} broken anchors")
    return 1 if broken else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
