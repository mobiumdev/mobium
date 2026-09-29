# Branding

| File | What it is |
| --- | --- |
| [mobium-mark.svg](mobium-mark.svg) | **The master.** The glyph alone, as vectors |
| [mobium-icon.svg](mobium-icon.svg) | App icon: the mark on the dark ground, inside a rounded square |
| [mobium-icon-light.svg](mobium-icon-light.svg) | The same icon on white |
| [mobium-icon-512.png](mobium-icon-512.png) | The app icon as a 512² PNG, transparent outside the rounded square — rendered from mobium-icon.svg by headless Chrome, for the MCP bundle |
| [favicon.svg](favicon.svg) | The mark at tab size |
| [mobium-logo.png](mobium-logo.png) | The original: glyph over the wordmark, on white, 1254² |
| [mobium-logo-variants.png](mobium-logo-variants.png) | The original's three settings — light, dark, app icon |

## Palette

The values, with the shape each belongs to. Taken from the SVGs rather than
from a color picker on the raster, so these are the definitions and not a
reading of them.

| Shape | Gradient | From | To |
| --- | --- | --- | --- |
| Upper-left wing | `wingL` | `#4C59FC` | `#3552FD` |
| Upper-right wing | `wingR` | `#0F70FC` | `#00BAFC` |
| Lower-left foot | `footL` | `#763CFB` | `#5832FA` |
| Lower-right foot | `footR` | `#02BCC1` | `#01CBDD` |

All four gradients are **horizontal** — `y1 = y2 = 0`, `userSpaceOnUse`, left
to right — which is what makes the set read blue-violet to cyan across the
glyph rather than top to bottom. Each is scoped to its own shape, so there is
no single gradient spanning the mark: the continuity is a consequence of the
four ranges being chosen to meet, not of one fill.

| Ground | Where | Value |
| --- | --- | --- |
| Dark | `mobium-icon.svg`, and the default | `#141826` |
| Light | `mobium-icon-light.svg` | `#FFFFFF` |
| None | `favicon.svg` | no `rect` — transparent |

The rounded square is `rx="114"` on a 512 viewBox, **22.3%** of the edge. Keep
the ratio rather than the number when rendering at another size.

**Deriving a UI palette.** Contrast against each ground, computed by the WCAG
formula rather than judged by eye:

| Color | On white | On `#141826` |
| --- | --- | --- |
| `#4C59FC` | 5.07:1 AA | 3.49:1 |
| `#3552FD` | **5.56:1 AA** | 3.18:1 |
| `#0F70FC` | 4.43:1 | 3.99:1 |
| `#00BAFC` | 2.23:1 | **7.93:1 AA** |
| `#763CFB` | 5.51:1 AA | 3.21:1 |
| `#5832FA` | **6.46:1 AA** | 2.74:1 |
| `#02BCC1` | 2.34:1 | 7.54:1 AA |
| `#01CBDD` | 1.98:1 | **8.91:1 AA** |
| `#141826` | 17.67:1 AA | — |

The set is **complementary, not universal**: the violets and blues pass AA on
white and fail on the dark ground; the cyans do the exact reverse. No color
here clears 4.5:1 on both, so a component that must work in light and dark
switches its accent rather than picking one that survives both.

For a single brand blue on white, `#3552FD` (the end of `wingL`) at 5.56:1 —
the best of the four wing stops. On the dark ground it is `#01CBDD` at 8.91:1.
`#141826` is the ink as well as the ground.

The trap is `#0F70FC`, which reads as the obvious "primary blue" and lands at
**4.43:1** — under AA by 0.07, which no eye will catch and every audit will.

**Which icon to use.** The dark one is the safer default: it reads on
anything. The light one has no border, so on a white page the rounded square
is invisible and only the mark shows — correct for an app icon, since iOS and
Android mask the corners and composite on their own background, but wrong for
a listing or a document on white. A hairline border would fix the second case
and spoil the first, so neither file has one and the choice stays with the
caller.

Both icons and the favicon are generated from `mobium-mark.svg`, and their
drawing elements are identical — checked, not assumed — so the set cannot
drift apart.

The two PNGs are the only ones the repository tracks: `*.png` is in
`.gitignore` because device screenshots land in the working tree constantly,
and these are excepted by name rather than the rule being loosened.

## How the vectors were made, and how good they are

Not by an autotracer. Each shape's outline was pulled out of the PNG by
connected-component labeling, and each curve fitted to those measurements —
the wings are two cubic Béziers apiece, the feet one cubic against a straight
edge. Then checked the only way worth checking: **97% of the mark by area
agrees with the raster**, and no fitted curve is more than 5px from it on a
585px mark.

An earlier attempt using circular arcs managed 89.9%, and the row-by-row
comparison said why — the outer edge of each wing is nearly straight at the
top and then turns, which no single arc can do. That is the kind of thing a
number tells you and an eye does not.

The result is eight curves instead of the thousands of points a trace
produces, so it can be edited by hand. The right half is the left reflected
about `x=628`, computed rather than retyped. The raster is asymmetric by a few
pixels and the vector is not, deliberately: that asymmetry is an artifact of
the original raster, not part of the design.

## What is deliberately not vectorized

**The wordmark.** It is a geometric sans whose typeface is not recorded here,
and hand-drawing letterforms from a raster produces something that looks
almost right, which is worse than not doing it. Use
[mobium-logo.png](mobium-logo.png) for the full lockup until the typeface is
known, then set it properly and convert to outlines.

The repository README uses the PNG rather than the SVG for the same sort of
reason: GitHub sanitizes inline SVG, and a mark that renders differently
there than everywhere else is not worth the smaller file.

The glyph is an **M** built from four leaves that read as a butterfly — the
two upper wings form the peaks, the two lower shapes the legs. Blue-violet to
cyan, left to right.

## Motto

***Mutatis mutandis.*** Latin, "the necessary changes having been made" — used
when an argument is carried into a new domain and only what must change does.

It is a statement of the architecture rather than a flourish, which is the
only reason it is worth having. Mobium is Vibium with the browser swapped for
a device; one locator vocabulary is compiled per platform instead of a dialect
per platform; one tool layer serves a CLI, an MCP surface and five clients; and
a third-party driver inherits waiting, scrolling and `@ref`s by changing only
the platform layer beneath them. Each of those is the same argument with the
necessary changes made. Its origin, in Poul Anderson's "The Three-Cornered
Wheel", is in [docs/PHILOSOPHY.md](../../docs/PHILOSOPHY.md).

Set it in italics, capitalized as a sentence — *Mutatis mutandis* — and do not
translate it inline in display use; the gloss belongs in body text, as above.
It pairs with the mark rather than replacing the descriptive line: the mark
says what it is called, "native app automation for AI agents and humans" says
what it does, and the motto says how it is built.

## Namespace

The Java client's group id is **`dev.mobium`**, from the `mobium.dev` domain,
which matches the Java package name.

## Use

Nothing in the tool renders these; they are for the README, a future site, and
package listings.

Still missing: a `.ico`, which some contexts still insist on. PNG exports are
made with the rasterizer macOS ships:

```sh
qlmanage -t -s 1024 -o <outdir> mobium-icon.svg   # -> mobium-icon.svg.png
```

It honors the viewBox, antialiases cleanly and preserves alpha (RGBA, checked
on `mobium-mark.svg`). That is how MobiumApp's icon and in-app mark were made.
Both are downstream of the SVG now rather than of the original raster, which
is the point of having done this.
