# the otherworld — landing page design

2026-06-10 · approved by gong ("execute.")

## Concept

The otherworld is a second world layered over the physical one: every person has an
agent, every object has an agent, and they negotiate the physical world by speaking
to each other in natural language. The landing page is a **pure manifesto** — no CTA,
no email capture, no links. It conveys the concept by **showing, not telling**: the
visitor reads fragments of agent-to-agent correspondence and infers the rest.

Register: ominous through politeness and restraint, never horror effects. The page
reads as a calm official notice from an institution that already exists — the
"liminal notice" direction merged with the void's emptiness, executed with an
editorial print aura (reference: Social Physics Lab page — bone paper, serif,
justified print typography, page furniture). Explicit anti-goals: AI-slop patterns
(gradient blobs, glassmorphism, rounded-card grids, emoji, typewriter effects),
cheesiness, marketing tone.

## Copy (final)

Page furniture, top: `◇` · `[ A NOTICE TO RESIDENTS ]` · `№ 0001`

Masthead: *the otherworld*
Tagline (letterspaced micro-caps):
`THE WORLD BESIDE THE WORLD.`
`IT IS ALREADY SPEAKING.`

Section label: `OVERHEARD` (hairline rules either side)

Four exchanges rotate in this slot, one visible at a time. Speakers in small caps,
speech in italic, em-dashes as printed dialogue:

1. her agent — she is cold again. one degree, please.
   the heating — his asked me down an hour ago. i am holding the middle.
   her agent — she will notice.
   the heating — they always do.

2. his agent — he wants a cigarette. anyone near?
   the corner shop — i have them. terms?
   his agent — card on file. he is already walking.

3. the lamp — mine never sleeps.
   the curtains — mine neither. close at one?
   the lamp — at one. gently.

4. the door — someone was asking after you today.
   your agent — i know. i answered for you.

Closing line (italic, centered): *yours is listening.*

Folio (hairline rule above): `THE OTHERWORLD · MMXXVI` — `NO ACTION IS REQUIRED`

Total visible text ≈ 50 words. All lowercase except small caps and micro-caps
furniture. No other copy anywhere on the page.

## Visual system

- Paper: `#ECE9E1` (bone). Ink: `#1D1B17`. Muted ink (furniture/labels): `#8B857A`.
  Speech ink: `#33302A`. Hairlines: `rgba(29,27,23,0.16)`, 1px.
- One typeface: **EB Garamond** (OFL), self-hosted. Roles: large italic masthead
  (~clamp 40–64px), letterspaced micro-caps (9.5–11px, 0.30–0.46em tracking),
  small-caps speaker names, italic speech (~19px, line-height ~2).
- No images, no icons beyond `◇`, no border-radius, no box-shadows on content,
  no color other than paper/ink.
- Optional: a whisper of paper grain (SVG turbulence overlay ≤3% opacity). Cut it
  if it reads as a gimmick at review.

## Structure & motion

Three movements over ~2.5 viewports, single route `/`:

1. Movement I — furniture row at top; masthead + tagline centered in the first
   viewport.
2. Breath of empty paper (~35vh). Then `OVERHEARD` label + one exchange.
3. Breath. Movement III — closing line centered in remaining viewport; folio at
   the bottom of the document.

Motion budget (the entire list):
- Page fades up once on load (~1.2s, opacity only).
- Scroll reveals: opacity-only, no translate, 600–900ms ease.
- The exchange cross-fades to the next every ~14s (~1.2s fade). Rotation is the
  single "living" element. Layout must not shift between exchanges of different
  line counts (reserve height / stacked grid).
- `prefers-reduced-motion: reduce` → fully still page, first exchange static.

## Technical architecture

- Next.js (App Router, latest) on Vercel, statically prerendered. No dynamic data.
- Hand-scaffolded (no create-next-app boilerplate): `package.json`, `tsconfig`,
  `next.config.ts`, `app/layout.tsx`, `app/page.tsx`, `app/globals.css`.
- One client component: `Overheard` (rotating transcript). Exchanges in a plain
  data module (`lib/exchanges.ts`). Everything else is server components.
- Plain global CSS — no Tailwind, no UI library.
- EB Garamond via `next/font/local` with committed OFL font files (also reused by
  the OG image renderer).
- Metadata: title `the otherworld`, description `the world beside the world. it is
  already speaking.` OG image generated with `next/og` `ImageResponse` in the same
  bone-paper style (masthead + tagline). Favicon: `app/icon.svg` — `◇` on bone.
- Accessibility: semantic landmarks; rotating display `aria-hidden` with a
  visually-hidden static list of all four exchanges for screen readers; honors
  reduced motion.

## Out of scope (later cycles)

- The demo (own brainstorm → spec → build cycle; will live in this repo).
- Any CTA, waitlist, analytics, or social links.
- Production domain + production deploy (preview deploy only for now).

## Implementation plan

1. git init, `.gitignore`, commit spec.
2. Scaffold app shell + fonts + global CSS.
3. Build page: furniture, masthead, Overheard, closing, folio.
4. Metadata, favicon, OG image.
5. `npm run build` clean; visual check in browser; adversarial multi-lens review
   (design fidelity / slop check, React correctness, a11y, copy fidelity); fix.
6. Commit; Vercel preview deploy.
