import re, sys
PAIRS = {
 # -our
 "behaviour":"behavior","behaviours":"behaviors","behavioural":"behavioral",
 "colour":"color","colours":"colors","coloured":"colored","colouring":"coloring",
 "favour":"favor","favours":"favors","favoured":"favored","favourite":"favorite","favourites":"favorites","favourable":"favorable",
 "honour":"honor","honours":"honors","honoured":"honored","labour":"labor",
 "neighbour":"neighbor","neighbours":"neighbors","rumour":"rumor","humour":"humor",
 "armour":"armor","endeavour":"endeavor","flavour":"flavor","flavours":"flavors",
 "harbour":"harbor","odour":"odor","savour":"savor","vapour":"vapor","vigour":"vigor",
 # -re
 "centre":"center","centres":"centers","centred":"centered","centring":"centering",
 "metre":"meter","metres":"meters","theatre":"theater","fibre":"fiber","litre":"liter",
 "sombre":"somber","calibre":"caliber","manoeuvre":"maneuver",
 # -ce / -se
 "defence":"defense","offence":"offense","pretence":"pretense",
 # misc
 "catalogue":"catalog","catalogued":"cataloged","cataloguing":"cataloging",
 "programme":"program","programmes":"programs","grey":"gray","greyed":"grayed",
 "artefact":"artifact","artefacts":"artifacts","judgement":"judgment","judgements":"judgments",
 "licence":"license","licences":"licenses","practise":"practice","practising":"practicing",
 "ageing":"aging","analogue":"analog","focussed":"focused","storey":"story","kerb":"curb",
 "draught":"draft","plough":"plow","cosy":"cozy","offences":"offenses","defences":"defenses",
 "aluminium":"aluminum","sulphur":"sulfur","mould":"mold","moulded":"molded",
 "whilst":"while","amongst":"among","learnt":"learned","spelt":"spelled","dreamt":"dreamed",
 "travelled":"traveled","travelling":"traveling","traveller":"traveler",
 "cancelled":"canceled","cancelling":"canceling","labelled":"labeled","labelling":"labeling",
 "modelled":"modeled","modelling":"modeling","signalled":"signaled","totalled":"totaled",
 "fuelled":"fueled","marvellous":"marvelous","jewellery":"jewelry",
 "enrol":"enroll","fulfil":"fulfill","fulfilment":"fulfillment","instalment":"installment",
 "skilful":"skillful","wilful":"willful","distil":"distill","instil":"instill",
 "enquire":"inquire","enquiry":"inquiry","sceptic":"skeptic","sceptical":"skeptical",
 "cheque":"check",
 # -ise / -isation
 "materialise":"materialize","materialised":"materialized","materialising":"materializing",
 "realise":"realize","realised":"realized","realising":"realizing","realisation":"realization",
 "recognise":"recognize","recognised":"recognized","recognising":"recognizing",
 "organise":"organize","organised":"organized","organising":"organizing","organisation":"organization","organisations":"organizations","organisational":"organizational",
 "normalise":"normalize","normalised":"normalized","normalising":"normalizing","normalisation":"normalization",
 "serialise":"serialize","serialised":"serialized","serialising":"serializing","serialisation":"serialization",
 "initialise":"initialize","initialised":"initialized","initialising":"initializing","initialisation":"initialization",
 "capitalise":"capitalize","capitalised":"capitalized","capitalisation":"capitalization",
 "summarise":"summarize","summarised":"summarized","summarising":"summarizing",
 "prioritise":"prioritize","prioritised":"prioritized","utilise":"utilize","utilised":"utilized",
 "customise":"customize","customised":"customized","optimise":"optimize","optimised":"optimized","optimisation":"optimization",
 "synchronise":"synchronize","synchronised":"synchronized","synchronisation":"synchronization",
 "authorise":"authorize","authorised":"authorized","authorisation":"authorization",
 "minimise":"minimize","minimised":"minimized","maximise":"maximize","maximised":"maximized",
 "emphasise":"emphasize","emphasised":"emphasized","specialise":"specialize","specialised":"specialized",
 "criticise":"criticize","criticised":"criticized","sanitise":"sanitize","sanitised":"sanitized","sanitising":"sanitizing",
 "apologise":"apologize","apologised":"apologized","standardise":"standardize","standardised":"standardized",
 "categorise":"categorize","categorised":"categorized","characterise":"characterize","characterised":"characterized",
 "stabilise":"stabilize","stabilised":"stabilized","visualise":"visualize","familiarise":"familiarize",
 "generalise":"generalize","itemise":"itemize","localise":"localize","localised":"localized","localisation":"localization",
 "modernise":"modernize","neutralise":"neutralize","penalise":"penalize",
 "randomise":"randomize","randomised":"randomized","rationalise":"rationalize","tokenise":"tokenize",
 # -s forms. The list grew verb by verb and the third-person singular was
 # missed throughout, so `sanitises` passed while `sanitised` did not. Note
 # "analyses" is deliberately absent: in American English it is also the
 # plural of "analysis", and flagging it would be wrong more often than right.
 "realises":"realizes","recognises":"recognizes","organises":"organizes",
 "normalises":"normalizes","serialises":"serializes","initialises":"initializes",
 "summarises":"summarizes","synchronises":"synchronizes","authorises":"authorizes",
 "minimises":"minimizes","maximises":"maximizes","emphasises":"emphasizes",
 "specialises":"specializes","criticises":"criticizes","standardises":"standardizes",
 "categorises":"categorizes","characterises":"characterizes","utilises":"utilizes",
 "customises":"customizes","optimises":"optimizes","localises":"localizes",
 "prioritises":"prioritizes","sanitises":"sanitizes","stabilises":"stabilizes",
 "visualises":"visualizes","generalises":"generalizes","itemises":"itemizes",
 "modernises":"modernizes","neutralises":"neutralizes","penalises":"penalizes",
 "randomises":"randomizes","rationalises":"rationalizes","tokenises":"tokenizes",
 # -ing forms the list skipped
 "optimising":"optimizing","utilising":"utilizing","emphasising":"emphasizing",
 "specialising":"specializing","criticising":"criticizing","standardising":"standardizing",
 "categorising":"categorizing","characterising":"characterizing","minimising":"minimizing",
 "maximising":"maximizing","customising":"customizing","prioritising":"prioritizing",
 # vectorise, which was absent entirely
 "vectorise":"vectorize","vectorised":"vectorized","vectorising":"vectorizing","vectorises":"vectorizes",
 "analyse":"analyze","analysed":"analyzed","analysing":"analyzing","analyser":"analyzer",
 "paralyse":"paralyze","catalyse":"catalyze",
}
PAIRS = {k:v for k,v in PAIRS.items() if not v.endswith("_KEEP")}

def case_like(src, repl):
    if src.isupper(): return repl.upper()
    if src[0].isupper(): return repl[0].upper()+repl[1:]
    return repl

RX = re.compile(r"\b(" + "|".join(sorted(PAIRS, key=len, reverse=True)) + r")\b", re.IGNORECASE)

def fix(text):
    return RX.sub(lambda m: case_like(m.group(0), PAIRS[m.group(0).lower()]), text)

# Files whose British spellings are the subject rather than a mistake. A tool
# whose input vocabulary is the thing it removes will consume itself, and it
# will look like it worked — see CHALLENGES, "a spelling sweep that ate its
# own documentation". CHALLENGES.md quotes six of these words to record that.
SKIP = {"american-spelling.py", "CHALLENGES.md"}

if __name__ == "__main__":
    if len(sys.argv) < 3:
        # Without paths this used to scan nothing and print "0 files, 0
        # occurrences" — indistinguishable from a clean sweep, which is the
        # same shape as the two defects this tool already has recorded
        # against it. Refuse instead.
        # The glob list is part of the check. An earlier sweep here omitted
        # '*.sh' and reported the repository clean while `normalises` sat in
        # docs/checks/clock-timer.sh -- the same incompleteness this tool has
        # already been caught by twice. Prefer the no-glob form below.
        print("usage: american-spelling.py {scan|write} <file>...\n"
              "  sweep everything tracked, without having to list extensions:\n"
              "    python3 docs/checks/american-spelling.py scan $(git ls-files)",
              file=sys.stderr)
        sys.exit(2)
    mode = sys.argv[1]
    hits = {}
    for path in sys.argv[2:]:
        if path.rsplit("/", 1)[-1] in SKIP:
            continue
        try: s = open(path, encoding="utf-8").read()
        except Exception: continue
        found = RX.findall(s)
        if not found: continue
        hits[path] = found
        if mode == "write":
            open(path, "w", encoding="utf-8").write(fix(s))
    for p, f in sorted(hits.items()):
        from collections import Counter
        print(f"{p}: " + ", ".join(f"{w}×{n}" for w, n in Counter(x.lower() for x in f).most_common()))
    print(f"\n{len(hits)} files, {sum(len(v) for v in hits.values())} occurrences")
    # scan exits non-zero when it finds something, so it can gate a build.
    # It used to exit 0 whatever it found, which made `scan ... || fail` a
    # decoration -- a `make` target built on it reported "clean" with
    # `colour behaviour` planted in a tracked file. That is the fourth variant
    # of this tool's recurring defect and the first one caught before shipping,
    # by planting a violation instead of trusting a pass.
    # `write` exits 0 on purpose: it found them and fixed them.
    if mode == "scan" and hits:
        sys.exit(1)
