# the otherworld — landing manifesto redesign

2026-06-11 · approved by gong ("execute")

## Concept

Revision of the 2026-06-10 landing page. The first cut was pure atmosphere —
fifty words of mood and an ARG-teaser whisper. Verdict: cheesy. The page becomes
a **concise manifesto**: concrete about the *result* the otherworld delivers,
silent about implementation (no agents-architecture talk, no why-now argument —
that stays in MISSION.md). Form chosen from three drafts: **thesis + terms** —
one concrete thesis paragraph, then six one-line terms. The form enacts the
product: a notice that settles in terms.

Kill list (cut everywhere, including metadata and the OG image):

- `it is already speaking.` (tagline line 2)
- `yours is listening.` (closer — deleted, not replaced; term 6 closes the page)
- `no action is required` (folio right span and OG footer)
- the door exchange (`someone was asking after you today`) — surveillance-cute
- `gently.` in the lamp exchange

Visual system (bone paper, EB Garamond, hairlines, furniture) is unchanged.
`the world beside the world.` survives as the only poetic line.

## Copy (final)

Furniture, top (unchanged): `◇` · `[ A NOTICE TO RESIDENTS ]` · `№ 0001`

Masthead: *the otherworld*
Tagline (one line): `THE WORLD BESIDE THE WORLD.`

Thesis (upright roman, justified):

> every person has an agent; every thing has one too. the radiator has
> standing, the door can answer, the corner shop can quote terms — and the
> small constant business of living together settles itself, on a record
> anyone affected can read.

Section label: `THE TERMS` (hairline rules either side)

1. nothing is done in your name beyond a mandate you gave, can read, and can revoke.
2. everything said in your name is yours to read.
3. nothing merely commands; nothing merely obeys.
4. the door can forget.
5. your agent answers to you alone.
6. no one may own the air.

Section label: `OVERHEARD`. Three exchanges rotate (door exchange cut); each
now ends with a settlement record in bracket-furniture style — the line that
turns the dialogue from ambient whimsy into a concrete result:

1. her agent — she is cold again. one degree, please.
   the heating — his asked me down an hour ago. i am holding the middle.
   her agent — she will notice.
   the heating — they always do.
   `[ SETTLED · 20.5° UNTIL MORNING ]`

2. his agent — he wants a cigarette. anyone near?
   the corner shop — i have them. terms?
   his agent — card on file. he is already walking.
   `[ SETTLED · ONE PACK · CARD ON FILE ]`

3. the lamp — mine never sleeps.
   the curtains — mine neither. close at one?
   the lamp — at one.
   `[ SETTLED · CURTAINS AT 01:00 ]`

Folio: `THE OTHERWORLD · MMXXVI` (single span; right span removed)

## Structure & typography

Three movements, same skeleton, smaller voids (`.breath` 34svh → 18svh):

1. Movement I — furniture; masthead + one-line tagline, first viewport.
2. Movement II — thesis, `THE TERMS` label, the six terms.
3. Movement III — `OVERHEARD` label + rotating exchanges, centered; folio at
   document bottom.

New CSS roles, inside the existing system (no new colors, faces, or effects):

- `.thesis` — upright roman, 1.1875rem, line-height ~1.9, `--ink-speech`,
  `width: min(34rem, calc(100% - 3rem))` centered, justified with
  `hyphens: auto`. Upright against the italic speech: statement, not whisper.
- `.terms` — same measure; `list-style: none` + CSS counter, hanging numerals
  in `--ink-mute` (oldstyle figures inherit from body).
- `.settled` — composed with `.micro` (caps, 0.3em tracking, `--ink-mute`),
  margin above; brackets in content text.
- `.overheard-label` renamed `.ruled-label` (it now labels two sections).
- `.closing` deleted (unused).

Rotation mechanics untouched: 14s interval, 1.2s cross-fade, hover pause,
`prefers-reduced-motion` honored live, stacked-grid height reservation, SSR
ships visible. Screen-reader list gains the settlement lines.

## Metadata & OG image

- `layout.tsx` description → `the world beside the world.`
- `opengraph-image.tsx`: alt → `the otherworld — the world beside the world.`;
  remove the `IT IS ALREADY SPEAKING.` tagline line and the
  `NO ACTION IS REQUIRED` footer block. Masthead + tagline + furniture remain.
- Font subsets verified: `°`, `:`, `·`, digits all present (208-glyph cut).

## Blast radius

`app/page.tsx` · `lib/exchanges.ts` (exchange gains `settled` field) ·
`app/components/Overheard.tsx` · `app/globals.css` · `app/layout.tsx` ·
`app/opengraph-image.tsx`. No new dependencies.

## Out of scope

The demo, any CTA/waitlist/analytics, production domain work. Preview deploy
only (push branch; Vercel builds the preview).

## Implementation plan

1. Commit this spec.
2. `lib/exchanges.ts`: new shape `{ lines, settled }`, three exchanges, copy edits.
3. `Overheard.tsx`: render settled line in stack and sr-only list.
4. `page.tsx`: new structure (tagline trim, thesis, terms, folio single span).
5. `globals.css`: `.thesis`, `.terms`, `.settled`; rename `.ruled-label`;
   shrink `.breath`; delete `.closing`.
6. `layout.tsx` + `opengraph-image.tsx` metadata cuts.
7. `npm run build` clean; verify prerendered HTML contains the terms and none
   of the kill-list lines; commit; push branch for Vercel preview.
