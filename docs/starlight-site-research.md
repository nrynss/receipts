# Receipts Starlight site research

Status: research and implementation complete. Browser review passed. Public deployment is being verified.
The user requested research before delegating construction. No demo script is
needed: the existing user-prepared script stays untouched.

## Reference inspected

Ribhu lives at `/home/nryn/work/showcase/site/`, rather than a separate ribhu
repository. Its Astro configuration uses Starlight, a splash homepage, a manual
sidebar, a custom CSS theme, dark/light logos, MDX components and a Mermaid SVG
remark plugin. The observed pins are Astro 7.4.0 and Starlight 0.42.6; these are
reference project versions, not a claim about latest registry releases.

Its useful pattern is a product introduction followed by a judge walkthrough,
architecture, production information and linked evidence. Repository Markdown
is rendered at build time so design and evidence have a single source. Its CI
builds the site, checks generated links, then publishes static output through
GitLab Pages. The deployed URL in the configuration is
https://showcase-5631db.gitlab.io/; the web research tool could not access that
origin, so these findings are based on source inspection rather than a fresh
visual verification of the deployed site.

Use this organization and the published Starlight framework as references.
Write Receipts-specific code and content; do not copy Ribhu's custom components,
plugin or content loader into this hackathon project.

## Visual direction

Use the existing Receipts wrestling-poster and paper-slip design. The title
card says THE STREAMER VS CHAT, RECEIPTS and The stream remembers what you said.
Use green canvas, cream paper, gold accents, crimson stamps, hard shadows and
small uppercase labels. Ring ropes can frame the homepage hero and the footer;
long technical pages should remain calm and readable. Prefer the existing
Receipts mark from web assets/components over introducing a new logo.

Tokens in demo-shoot/receipts/design.md and slides/cards.html:

| Role | Value |
|---|---|
| Canvas | #101713 |
| Panel | #1a241e |
| Inset | #131b16 |
| Paper | #f4eedc |
| Ink | #171d1a |
| Shadow | #080b09 |
| Stamp | #b42e25 |
| Accent | #f1b969 |
| Border | #535b50 |

Map these to Starlight CSS variables and define a readable warm-paper light
theme as well. Use semantic HTML for headings and commands. Rebuild the six-step
pipeline as responsive HTML/SVG with accessible text; a 1920-pixel screenshot
alone is illegible on a phone. Preserve reduced-motion behavior. Animations
should be optional, muted, finite and accompanied by still posters.

## Available assets and proposed use

Asset root: `/home/nryn/work/demo-shoot/receipts/`.

| Source | Proposed use | Caveat |
|---|---|---|
| screenshots/cards/title.png; video/clips/title.webm | Homepage hero/poster | Illustration, not live proof |
| screenshots/cards/problem.png; definition.png | Explain the idea | Keep wrestling terminology brief and understandable |
| screenshots/cards/pipeline.png; video/clips/pipeline.webm | Capture architecture | Responsive recreation from original slide design; add judging path separately |
| screenshots/cards/match.png; rule.png | Retrieval, judgment and owner approval | Illustration; include manual review boundary |
| screenshots/cards/rules.png | Safety and owner controls | Qualify speaker attribution as sole-voice restriction, not unrestricted diarization |
| screenshots/cards/stack.png; video/clips/stack.webm | Built with Codex / stack page | Illustration; exact version/model details come from source docs |
| screenshots/13-B-chat-reply.png | Real command and confirmation evidence | Crop to relevant conversation, preserve usernames/text; no false live label |
| screenshots/12-A-dock-after-receipt.png; 20-A-dock-populated.png | Saved claim and owner controls | Check each selected image before publishing |
| screenshots/21-B-control-while-A-populated.png | Owner isolation example | Pair with written evidence; empty B alone is insufficient proof |
| screenshots/22-A-dock-review.png | Historical pre-fix state only | Inspected: it shows NO review items, despite the filename |
| takes/04-receipt-captured-PASS.mkv | Selected real capture excerpt | Disclose synthetic stock promise voice; not a final edited film |
| takes/05-red-card-no-detection.mkv | Optional bug/fix story | Failed pre-fix take; never label successful contradiction detection |
| takes/2026-11-03 01-55-47.mkv | Candidate later-run recording | Inspected one early frame only: Blue or Red with unavailable dock; ffprobe did not report duration; verify/finalize before selecting excerpts |
| script.md; design.md; beats.json | Existing editorial and design references | Script is user-owned/completed; do not rewrite it |

There are 11 VP8 WebM card clips, all 1920x1080, approximately 3.4 to 24 seconds
each, totaling about 6.7 MB. The card PNGs and screenshots total about 3 MB.
The full asset folder is roughly 200 MB including raw takes, voice models and
audio; do not copy the whole directory into the site.

No `video/receipts-demo.mp4` exists in the inspected folder. Do not create a
broken Watch demo button or call card animations the finished demo film.
Keep a watch-video slot conditional on a verified final artifact or URL.
Do not publish voice models, voice-generation scripts, capture tools, private
logs or raw browser/account recordings. The two inspected chat/control stills
showed no visible credentials; that does not clear all other assets.

## Proposed information architecture

| Page | Content and assets |
|---|---|
| Overview | Match-card hero; short premise; Try recorded demo, Judge walkthrough, Connect Twitch CTAs; evidence summary |
| Judge walkthrough | Recorded fixture versus real run; command, clip, owner review, vote, verdict and lore; screenshots and clip link |
| Connect Twitch and OBS | Owner login, four-character pairing, 1920x1080 source, sole-voice checkbox, viewer commands, one-active-channel hosting limit |
| How it works | Capture pipeline, exact source sentence, retrieval, Decisions classification, private review, appeal and SSE |
| Safety and limits | Sensitive-topic handling, owner approval, pause versus listening stop, void, account isolation, spending bounds and known limits |
| Evidence and evaluation | Curated live results, T5.4 reports, issue/fix trail, passed versus unverified observations |
| Built with Codex | Verified principal session ID, concrete implementation commits, stack roster, user-supplied footage when available |
| Run locally and operations | Keyless fixture/replay commands, configuration names, hosting, backup/restore and contribution commands |

The public app remains https://receipts.nryn.dev/ with its /demo/, /account,
/control and /obs routes. Documentation links to these routes; it must not
replace the Svelte app or carry owner/device credentials.

## Content source and honesty rules

Use existing docs/submission.md, docs/development.md, docs/obs.md,
docs/operations.md, docs/model-migration.md and a curated evidence allowlist as
the source of factual content. Use an independently implemented build-time
import/generation step or Starlight content loader to avoid manual copies that
drift. Rewrite relative Markdown links for the site. Do not ingest every file
in docs/evidence automatically: large raw JSON recordings and historical spend
snapshots should not dominate the public site or search results.

Maintain an asset manifest with original source, output filename, status
(illustration, recorded demo, live capture or historical failure) and caption.
Do not infer a successful stage from a filename.

Verified: core live flow through real capture, ready clip, owner-spoken later
line, review, explicit approval, OBS verdict, vote, upheld result, lore and void.
Unverified: actual final OBS clip playback versus labelled fallback, and the
latest hosted restart observation. Source refresh was needed after deployment.
The original promise was a disclosed stock synthetic voice. Do not advertise
unrestricted speaker identification, automatic public stamps or prospective
model accuracy. Current hosted app budget ceilings are $20 global/channel/
session, cumulative global accounting; judges supply no API key or payment.
Avoid displaying a remaining-budget number as live unless actually refreshed.
The principal Codex build session ID is
01a187f1-2866-7273-aa07-ea214669f2b4, verified through the Codex app.

## Technical and deployment recommendation

Create a standalone `site/` Astro/Starlight project in the Receipts repository.
Use MDX and a small custom CSS layer with a splash homepage; retain Starlight's
search, sidebar, keyboard navigation and accessible documentation shell. Use
component overrides only where configuration and CSS cannot express the hero.
Pin a compatible stable Astro/Starlight pair after checking current releases.
No Svelte runtime or live provider client is needed in the docs site.

Use src/assets for optimized stills and an allowlisted public/media directory
for optional video. Provide explicit width/height, responsive sizes, alt text,
posters, lazy loading below the fold and native video controls. Do not start
Twitch embeds or download all animation clips on initial page load. Keep full
live recordings external or separately hosted when approved.

Recommend https://docs.receipts.nryn.dev/ as a proposed separate docs origin,
served by a small static container through the existing Hetzner Traefik setup.
No Compose, app database access, API secrets or new paid hosting is needed.
Prepare a reviewed image and routing plan before DNS/public host changes.
The hostname is proposed, not configured or approved by this research.

The Receipts repository is private. GitHub Pages is an alternative only after
checking plan entitlement and desired public publication behavior. GitHub's
own docs say private-source Pages needs an eligible paid plan. The personal
site already uses https://nryn.dev, so do not assume an unconfigured default
nrynss.github.io/receipts URL will be the final public route.

## Agent implementation brief and verification

Implement only after the research is accepted. Keep the existing app and demo
script unchanged. Agent owns site/, its asset manifest and site build/link
verification, plus small README navigation changes. Parent reviews and lands.
No live capture, model calls, Twitch messages, secrets transfer, production
routing or DNS mutations are part of the initial site build.

Acceptance:

- Every recommended page exists and has useful judge-first content.
- All reused assets have provenance/captions and appropriate publication review.
- No broken video CTA or misleading live/proof label.
- Mobile layouts work without squashed diagrams or horizontal overflow.
- Dark/light themes, keyboard navigation and reduced motion work.
- Local site build and generated link/asset checks pass.
- Site build requires no environment secrets or provider calls.
- Existing ./tools/check.sh still passes; document dependency installation
  separately from offline verification.
- Browser review covers landing, walkthrough, setup, evidence, search and 404.
- Public deployment is a separate concrete reviewed step.

## Official references checked

- https://starlight.astro.build/guides/customization/
- https://starlight.astro.build/guides/css-and-tailwind/
- https://starlight.astro.build/guides/overriding-components/
- https://starlight.astro.build/reference/configuration/
- https://starlight.astro.build/reference/frontmatter/
- https://docs.astro.build/en/guides/images/
- https://docs.astro.build/en/guides/deploy/github/
- https://docs.github.com/en/pages/getting-started-with-github-pages/what-is-github-pages
