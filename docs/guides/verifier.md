# The verifier: checking a claim about a recording

A test asserts what it can assert directly: an element appears, a label
reads "Card 3, Blue Morpho". Some claims are about what a person would have
seen across a whole session, and those are easier to state than to write
as assertions. The verifier judges such a claim from a recording, in a
fresh context that has seen nothing but the recording, and answers
**passed**, **failed** or **inconclusive** with the calls it rests on.

It is Vibium's Check carried over to Mobium. Three commands, and the same
three tools over MCP and in every client:

| Command | Tool | What it does |
| --- | --- | --- |
| `mobium recording` | `app_recording` | Reads a trace's .zip: a summary, the calls, one call, the screen after a call, its screenshot, a search. Read-only |
| `mobium verdict` | `app_verdict` | Gives a verdict and holds it to the verifier's rules |
| `mobium check` | `app_check` | Asks a model to read the recording and give the verdict |

None of them needs a device: a recording is enough. A check against a live
device is not built yet.

**A model's judgment is not proof.** Keep `wait` and test assertions for
what can be asserted directly, and use the verifier for the rest.

## A recording

Any trace works: `mobium trace start`, then what the claim is about, then
`mobium trace stop -o run.zip`, or `mobium test --trace on`, which saves one
per test. The examples below use one test of a memory game, recorded by
`mobium test`: it opens a seeded deal, taps the third card and waits for it
to read "Card 3, Blue Morpho".

## Being the judge yourself

`recording` shows what a judge sees. Every output below is what mobium
printed.

```sh
$ mobium recording summary -i card.zip
{"calls":9,"failed":[],"title":"match.test.json › a card turned over says what it is","tools":["app_wait_for ×4","app_open_url ×2","app_launch ×1","app_tap ×1","app_terminate ×1"]}

$ mobium recording search "Blue Morpho" -i card.zip
{"calls":[{"id":"call@7","ok":true,"title":"tap testid=card_2","tool":"app_tap"},{"id":"call@8","ok":true,"title":"wait_for label=\"Card 3, Blue Morpho\"","tool":"app_wait_for"},{"id":"call@9","ok":true,"title":"wait_for testid=moves","tool":"app_wait_for"}],"offset":0,"total":3}
```

`recording screen call@7` gives the screen after the tap as `map` gave it,
with each element's box as a share of the screen; `recording screenshot
call@7 -o after.jpg` saves what it looked like.

`verdict` takes the judgment and checks it. Passed or failed must cite at
least one call the recording holds, by id:

```sh
$ mobium verdict "After card 3 was tapped, it was shown face up as the Blue Morpho." -i card.zip \
    --status passed --summary "Card 3 reads Blue Morpho on the screen after the tap" \
    --evidence "call@7: after tap testid=card_2 the screen shows @e4 Card 3, Blue Morpho"
PASSED: Card 3 reads Blue Morpho on the screen after the tap
  - call@7: after tap testid=card_2 the screen shows @e4 Card 3, Blue Morpho

$ mobium verdict "After card 3 was tapped, it was shown face up as the Blue Morpho." -i card.zip \
    --status passed --summary "looks right"
error: the verdict does not hold to the rules: passed needs observable evidence
next: app_verdict again with the evidence citing calls by id (call@3) from app_recording
```

The second exits 2. This pair is how an agent you already run, such as one
in Claude Code, judges a claim with no model credential of Mobium's own:
give it the claim and the recording, and tell it to answer through
`app_verdict`.

## Asking a model

`mobium check` hands the same tools to a model in a fresh context.
`--model openai:<name>` asks one through the OpenAI-compatible API, which is
Ollama on this machine by default, with no credential:

```sh
$ mobium check "After card 3 was tapped, it was shown face up as the Blue Morpho." \
    -i card.zip --model openai:qwen2.5:14b
asking the model about the claim...
the model has made 2 reads; asking it again...
the model has made 3 reads; asking it again...
PASSED: After card 3 was tapped, it was shown face up as the Blue Morpho.
  - call@8: wait_for label="Card 3, Blue Morpho"
  - Screen after call@8 shows Card 3, Blue Morpho Blue Morpho (button)
(openai:qwen2.5:14b read /tmp/card.zip in 4 steps; a model's judgment, not proof)
```

That took 40 seconds on an Apple Silicon Mac. `MOBIUM_AI_BASE_URL` names
another server and `MOBIUM_AI_API_KEY` its key. **The recording is then
sent to that server**: its screenshots and screens. Send only recordings
of apps and devices that hold nobody's data.

With no `--model`, a check asks Claude, `claude-opus-5-5`, through the
Anthropic API. It needs an Anthropic credential (`ant auth login`, or
`ANTHROPIC_API_KEY` in the shell that starts the daemon), and each check is
a paid call. **This judge is built but not yet measured** against the claims
below. Without a credential, the check refuses and says so:

```sh
$ mobium check "After card 3 was tapped, it was shown face up as the Blue Morpho." -i card.zip
error: a check asks a model, and no Anthropic credential is set up on this machine
next: run `ant auth login`, or export ANTHROPIC_API_KEY in the shell that starts mobium and then `mobium daemon stop` so the daemon starts again with it
```

## How good is a judge

Measured on 28 claims about recordings of the memory game, each with an
answer settled by a deterministic fact (an assertion that passed or failed,
a pinned deal, a bug known to be on), never by someone reading a screen:
15 true, 10 false, 3 the recording cannot settle. Twelve are harder: a
color only the screenshot shows, a moment the recording never caught.

| Judge | Right | Confidently wrong |
| --- | --- | --- |
| Claude Code subagents, through `recording` and `verdict` | 28 / 28 | 0 |
| `openai:qwen2.5:14b`, Ollama | 17 / 28 | 0 |
| `openai:mistral-small:24b`, Ollama | 13 / 28 | 6 (5 false claims passed) |
| `openai:nvidia/nemotron-3-ultra-550b-a55b:free`, OpenRouter, on mistral's 6 wrong ones only | 6 / 6 | 0 |

What it says:

- **A judge is useful when it is never confidently wrong.** Inconclusive is
  an honest answer; a false passed defeats the purpose. qwen is safe but
  timid: every miss is inconclusive.
- **A cited verdict is not a true one.** mistral passed five false claims,
  twice with a summary saying the opposite of the claim, and its citations
  were real, so the citation rule could not catch it.
- The subagents are the same model family as the one that wrote the claims,
  so they are not an independent judge.
- Ollama's chat API takes images only in a user's turn, so local models are
  never shown a screenshot.

## Rules every verdict is held to

The same for a model's answer and for yours:

- Status is passed, failed or inconclusive.
- Passed or failed cites at least one call the recording holds, by id.
  A citation of a key the recording has not got is refused by name.
- At most 12 observations.
- Nothing missing is taken for success: a claim the recording cannot
  settle is inconclusive.
- A model that answers in prose instead of a verdict is asked again.
