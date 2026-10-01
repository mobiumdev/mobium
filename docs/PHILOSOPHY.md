# Philosophy

***Mutatis mutandis*** — Latin, "the necessary changes having been made."
Mobium's motto, and its one design rule: when something must work in a new
place — a second platform, a second front door, a third-party driver — the
answer is the thing that already works, with only the necessary changes. Not
a parallel implementation.

## Contents

- [Where it comes from](#where-it-comes-from)
- [What we took from it](#what-we-took-from-it)
- [The rule, applied](#the-rule-applied)
- [Facts of life that do not change](#facts-of-life-that-do-not-change)
- [How to use it](#how-to-use-it)
- [Sources](#sources)

## Where it comes from

The motto comes from Poul Anderson's *The Trouble Twisters* (1966), a
collection of his David Falkayn stories. The book opens with a one-page
prelude, "A Note of Leitmotif," set directly before its first story and written
as an extract from an in-universe work: Vance Hall's *Commentaries on the
Philosophy of Noah Arkwright*.

The prelude lists impossibilities that did not last — power from the atomic
nucleus, ray guns, artificial gravity, faster-than-light travel — each proved
out of reach until someone found the clause in fine print. Then it turns: some
things, Hall insists, do not change.

> I hereby state, flatly and unequivocally, that some facts of life are
> eternal. They are human facts, to be sure. *Mutatis mutandis*, they probably
> apply to each intelligent race on each inhabited planet in the universe; but
> I do not insist on that point.
>
> — Vance Hall, *Commentaries on the Philosophy of Noah Arkwright*, in Poul
> Anderson, "A Note of Leitmotif," *The Trouble Twisters* (1966)

The facts he means are Parkinson's Laws, Sturgeon's Revelation ("ninety
percent of everything is crud"), Murphy's Law, and a "Fourth Law of
Thermodynamics": everything takes longer and costs more.

That is the sense Mobium takes the phrase in. The same facts hold on every
world, with the necessary changes made for each: not one set of rules per
planet, but one set, carried.

### The story it introduces

The story that follows it, **"The Three-Cornered Wheel"** (first published in
*Analog*, October 1963), shows what carrying an idea into a hostile place looks
like. The traders David Falkayn and Martin Schuster are stranded on the planet
Ivanhoe, and the parts they need must be hauled a long way overland. The local
theocracy holds the circle sacred: nothing round may be put to mundane use, so
the wheel — the obvious answer — is forbidden.

The traders keep what a wheel is *for* and change the one thing the law
forbids. They roll the load on rollers of **constant width**: shapes that are
not circles but hold whatever rests on them at the same height all the way
round. The simplest is the Reuleaux triangle — draw an equilateral triangle,
then from each corner draw the arc through the other two. As the story puts
it, "The circle is merely a limiting case."

## What we took from it

**The constraint changes the shape, not the job.** A constant-width roller
does exactly what a wheel does for the load on top of it; only its outline is
different. That is the relation Mobium keeps between its platforms. Android,
iOS and a third-party device are three differently shaped rollers underneath,
and what rides on top — the tools, `map` and its `@ref`s, the locators, the
error codes, `start` and `quit` — stays at the same height on every one of
them.

**Change only what must change.** The traders did not reinvent transport; they
changed the one property the law cared about and kept everything else. Mobium
began the same way: it is [Vibium](https://github.com/VibiumDev/vibium)'s
architecture with the browser swapped for a device, and an agent that knows
Vibium needs no new concepts to use it.

**Constant width is a real property, not a resemblance.** The rollers work
because they genuinely hold the load level; a shape that merely looked round
would jolt it. So where a platform truly lacks something, Mobium refuses and
says so, rather than approximating. iOS has no back button, and Mobium does
not fake one with an edge swipe, because an app can tell the two apart.

## The rule, applied

Each of these is the same argument carried into a new domain, with the
necessary changes made:

| Carried from | Into | What changed |
| --- | --- | --- |
| Vibium's browser layer | `internal/mobiumdriver`, the one platform-specific layer | the device, not the browser; everything above it is written once |
| One tool layer | the CLI, the MCP server and five language clients | only the front door; the CLI parses flags and calls a tool by name |
| One way to read a web page | the WebView protocols — CDP on Android, WebKit's Remote Web Inspector on iOS simulators and iPhones — under one `Page` | only the wire; `map` and `text` are the same scripts |
| One locator vocabulary | every platform's element tree | compiled per platform, instead of a dialect per platform |
| The built-in drivers | third-party drivers, as separate processes | only the platform layer; waiting, scrolling and refs come for free ([the driver protocol](../examples/drivers/PROTOCOL.md)) |
| The Go client's connection | the Python, JavaScript, Java and .NET clients | each language's idiom: a lock, a timeout, the same failure rules |

The rule's negative form is the one that catches mistakes: **a proposal that
duplicates a layer is almost always the wrong one.** Before adding a second
path, find the first and ask what, exactly, must be different.

## Facts of life that do not change

Hall sets the impossibilities that fell against the facts of life that did
not, and holds that those carry, *mutatis mutandis*, to every world. Building
Mobium has turned up its own list of the second kind — behaviors that hold on
every platform and every tool, each written down in
[CHALLENGES.md](CHALLENGES.md) because it cost a defect first:

- A command that reports success is not evidence. Every device tool here
  reports some failures on stderr while exiting 0, so state is read back
  rather than believed.
- A command that starts something returns before the thing has started.
  Launching an app reports that the request was sent, not that the app is in
  front.
- A test that comes back "zero" or "no difference" is not a result until it
  has been shown it can come back non-zero.
- Fixtures cannot establish what a platform does. Most of the defects in the
  log were found only by running against a real device.

Murphy's Law is on Hall's list too, and it is worth having in its original
form. Project Rho's editors note that it began as a design rule rather than
a lament: if a part can be installed the wrong way, someone will, so design the
part so that there is no wrong way. That is the form Mobium uses. A locator
that matches two elements is refused rather than guessed at; an error names a
remedy only when that remedy can work; a tap on a control that ignores taps is
refused rather than reported as done.

## How to use it

When something needs to work somewhere new:

1. Find the thing that already works. It almost always exists.
2. Name what must be different in the new place — the platform, the wire, the
   language, the front door — and nothing else.
3. Change that, and keep the rest identical, down to the error codes and the
   words in the messages.
4. Where the new place genuinely lacks something, refuse and say so. Do not
   build a shape that only looks round.

## Sources

- Poul Anderson, *The Trouble Twisters* (1966): "A Note of Leitmotif" (p. 7)
  and "The Three-Cornered Wheel" (p. 9; first published in *Analog Science
  Fact — Science Fiction*, October 1963). Also in Baen's *The Van Rijn Method*
  (Technic Civilization Saga, book 1), where Hall's note introduces the story.
- [Atomic Rockets (Project Rho), "Preliminary
  Notes"](https://www.projectrho.com/public_html/rocket/prelimnotes.php#leitmotif)
  — "A Note of Leitmotif" reproduced in full, with the editor's note on
  Murphy's Law.
- [ISFDB: *The Trouble Twisters*](https://isfdb.org/cgi-bin/pl.cgi?334941=) —
  the collection's contents and page numbers.
- Alex Kasman, [Mathematical Fiction: *Three Cornered
  Wheel*](https://kasmana.people.charleston.edu/MATHFICT/mfview.php?callnumber=mf613),
  College of Charleston — the source of the quoted line from "The Three-Cornered
  Wheel."
- [ISFDB: *The Three-Cornered Wheel*](http://www.isfdb.org/cgi-bin/title.cgi?55407=)
  — publication history.
- [Poul Anderson Appreciation: "The Conclusions Of 'The Three-Cornered
  Wheel'"](http://poulandersonappreciation.blogspot.com/2020/03/the-conclusions-of-three-cornered-wheel.html)
  and ["Noah Arkwright"](http://poulandersonappreciation.blogspot.com/2013/05/noah-arkwright.html)
  — on the story and on Vance Hall and Noah Arkwright.
