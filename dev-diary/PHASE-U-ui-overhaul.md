# PU: UI, UX and copy overhaul

**Goal:** Unify the visual identity around the "Broadcast Ledger / Pro-Wrestling Receipt" aesthetic, fix streamer-dock and overlay UX gaps, and polish all user-facing copy before P6 deployment.

**Status:** complete (4/4)

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

---

## Tasks

### TU.1: Brand mark ("The Knife-Edge Chop"), design system and navigation shell

```yaml
requires: T3.1, TA.3
fixture-ok: yes
owns: web/src/lib/brand/, web/src/app.html, web/static/
status: complete
```

Introduce the unified **Dark Forest & Parchment Ledger** design system, the original **"Knife-Edge Chop"** SVG brand mark (`ReceiptMark.svelte` and `web/static/favicon.svg`), tactile button primitives, status pills and a shared top navigation bar for human-facing routes (`/`, `/demo/`, `/account`, `/control`). Keep `/obs` and `/?fixture=...` harness views free of navigation chrome and transparent when idle.

**Done when:** Every human-facing route shares the CSS token system, tactile button styles and brand header; `favicon.svg` and `ReceiptMark.svelte` render cleanly at `16×16`, `32×32` and hero scale; `/?fixture=...` and `/obs` remain transparent with no navigation chrome.

### TU.2: Broadcast overlay polish, landing page (`/`) and recorded judge demo (`/demo/`)

```yaml
requires: TU.1, T1.4, T2.4, T3.3
fixture-ok: yes
owns: web/src/lib/overlay/, web/src/lib/events/recovery.ts, web/src/routes/+page.svelte, web/src/routes/page.test.ts, web/src/routes/demo/, internal/demo/
status: complete
```

Build on the landed anonymous recorded-demo implementation (`431ca1e`); hosted `TA.4` acceptance remains after `T6.1` deployment. Polish `Overlay.svelte` and `recovery.ts` so a live `stamp` arrival animates and automatically reveals the `THE EVIDENCE` column (~1.5s after landing) instead of staying stuck in a single-column view. Remove duplicate full-quote rendering in the evidence pane when a clip iframe is mounted, add a visual tug-of-war vote bar for `appeal`, truncate visually noisy UUIDs in the header while keeping the full ID accessible, and apply the broadcast-ready copy table below. Replace the blank default view on `/` (when no `?fixture` parameter is present) with the full landing page: hero, 3-step explanation strip, primary CTAs (`/demo/` and `/account`) and an interactive 4-state `<Overlay />` preview switcher. Upgrade `/demo/` to mount the real `<Overlay />` with stage tabs (`1. Stamp`, `2. Evidence`, `3. Appeal Vote`, `4. Verdict`), a structured broadcast/chat timeline, a simulated Twitch chat reply box for `!lore`, and aligned claim text.

**Done when:** Visiting `/` without `?fixture` renders the full landing page and interactive `<Overlay />` preview; `/?fixture=sequence|stamp|evidence|appeal|verdict` continues to run the transparent harness; `/demo/` renders the live `<Overlay />` stages and structured timeline without provider calls; `overlay-checks.ts`, `page.test.ts`, `demo/page.test.ts` and `internal/demo` tests pass.

### TU.3: Streamer control dock (`/control`) and account/OBS setup (`/account`, `/obs`)

```yaml
requires: TU.1, T3.2, TA.3
fixture-ok: yes
owns: web/src/routes/control/, web/src/lib/control/, web/src/routes/account/, web/src/routes/obs/, internal/api/ledger.go, internal/store/channel_reads.go
status: complete
```

Bring `/control`, `/account` and `/obs` into the Dark Forest & Parchment Ledger design system and resolve five concrete UX gaps:

1. **Start-session parity in `/control`:** Add the sole-speaker confirmation checkbox (`sole_broadcaster`) to `/control` when `statusValue === 'not_listening' && canStart` and pass `{ sole_broadcaster: soleBroadcaster }` to `POST /account-api/start` so starting a session from the OBS dock can enable live speech-to-text attribution.
2. **Spoken contradiction evidence in `Quiet review`:** Update `Store.PendingReviews` (`internal/store/channel_reads.go`) to select `d.evidence_text` from `detections` and populate `ReviewItem.Reason` in `LedgerReader.ReviewQueueFor` (`internal/api/ledger.go`) with the spoken contradiction text (falling back to `"Possible contradiction needs review."` only if empty). Render both **THE PROMISE** (`item.claim`) and **HEARD NOW** (`item.reason`) on each review card in `/control`, with clear button hierarchy (**Approve** in `btn-stamp` and **Reject** in `btn-secondary`).
3. **Verified live-status display in `/control`:** Do not treat persisted `session.State == "live"` alone as fresh Twitch verification. Instead, derive `/control`'s live status display from the existing verified `/account-api/status` observation (which enforces `LiveStatusFreshness` via `session.Authority`) and `health.live_confirmed`, preserving `Unknown` whenever the observation is stale or `unknown`.
4. **Channel ledger & one-click Void + live appeal bar in `/control`:** Render `snapshot.receipts` in a compact **Channel Ledger** list with verdict badges, `@clipped_by` and a one-click **Void** button per receipt alongside the manual `#void-id` input form. Show a live appeal countdown and vote bar in **Current stamp** when `snapshot.current_appeal` is active.
5. **Account & OBS setup (`/account`, `/obs`):** Organize `/account` into three styled cards (**Broadcast & Listening Status**, **OBS Browser Source Pairing** with a one-click **Copy URL** button and timestamped/ID-tagged paired sources, and **Account & Danger Zone** separating `Sign out` from crimson danger actions `Disconnect Twitch` and `Delete account`). Add the SVG brand mark and **`RECEIPTS · OBS OVERLAY`** header to the unpaired `/obs` screen.

**Done when:** Starting a session from `/control` with sole-speaker checked enables transcription; pending review cards display the spoken contradiction text from `detections.evidence_text`; `/control` uses the verified status observation and preserves `Unknown` when stale; active receipts in `/control` support one-click voiding; `/account` provides one-click `/obs` URL copying and separated danger actions; all API, store and web tests pass.

### TU.4: Twitch chat feedback (`!receipt` & `!lore`) and full gate verification

```yaml
requires: TU.2, TU.3, T4.2
fixture-ok: yes
owns: internal/twitch/chat/, internal/lore/, docs/evidence/
status: complete
```

Update `internal/twitch/chat/router.go` so a successful `!receipt` capture (`res.Code == commands.ReplyCaptured && res.Receipt != nil`) sends a bounded, rate-limited chat confirmation (`🧾 Receipt taken (@<login>): "<claim>"`, truncated safely within `MaxMessageBytes` / 450 UTF-8 bytes) when `r.Sender != nil`, while remaining silent on `duplicate`, `throttled`, `no_eligible_claim`, `suppressed` and `transcript_unavailable`. Update `internal/lore/lore.go` so each listed claim in `!lore` includes its outcome tag in parentheses: `(open)`, `(broken)`, `(overturned)`, `(context changed)` or `(inconclusive)`, while preserving the scoreboard prefix (`Lore: <N> total | <open> open, <broken> broken, <overturned> overturned, <contextChanged> context changed, <inconclusive> inconclusive.`) and 450-byte UTF-8 ceiling. Run `./tools/check.sh` and the mounted browser verification across all routes.

**Done when:** `!receipt` sends a chat confirmation only on `ReplyCaptured` and respects rate limits; `!lore` annotates listed claims with their status tags within 450 UTF-8 bytes; `./tools/check.sh` and mounted browser checks pass with zero regressions.

---

## Visual System & Brand Specification

### 1. SVG Brand Mark: Option 1 ("The Knife-Edge Chop")

Use this exact vector artwork in `web/src/lib/brand/ReceiptMark.svelte` (accepting `size` / `class` props) and `web/static/favicon.svg`:

```xml
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" fill="none" aria-hidden="true">
  <!-- Hard Neo-Brutalist Offset Shadow -->
  <path d="M29 18 L37 24 L45 18 L53 24 L61 18 L69 24 L77 18 L85 24 L93 18 L103 108 L95 102 L87 108 L79 102 L71 108 L63 102 L55 108 L47 102 L39 108 L29 18 Z" fill="#080b09" transform="translate(6, 6)"/>

  <!-- Tilted Serrated Parchment Receipt Slip -->
  <g transform="rotate(-4 64 64)">
    <path d="M26 14 L35 21 L44 14 L53 21 L62 14 L71 21 L80 14 L89 21 L98 14 L98 112 L89 105 L80 112 L71 105 L62 112 L53 105 L44 112 L35 105 L26 112 Z" fill="#f4eedc" stroke="#171d1a" stroke-width="5" stroke-linejoin="round"/>
    <!-- Receipt Header Bar & Ledger Lines -->
    <rect x="35" y="27" width="54" height="8" fill="#171d1a" rx="1"/>
    <line x1="35" y1="43" x2="76" y2="43" stroke="#535b50" stroke-width="4" stroke-linecap="round"/>
    <line x1="35" y1="52" x2="64" y2="52" stroke="#b9b3a5" stroke-width="4" stroke-linecap="round"/>
    <line x1="35" y1="88" x2="72" y2="88" stroke="#b9b3a5" stroke-width="4" stroke-linecap="round"/>
    <line x1="35" y1="97" x2="88" y2="97" stroke="#171d1a" stroke-width="4" stroke-linecap="round"/>
  </g>

  <!-- Crimson Impact Starburst ("The Slap") -->
  <polygon points="64,28 73,47 96,36 84,57 110,64 85,74 98,94 74,83 66,104 56,82 32,93 45,72 18,64 44,55 33,35 55,46" fill="#b42e25" stroke="#171d1a" stroke-width="4.5" stroke-linejoin="round"/>

  <!-- Inner Gold Shockwave Star -->
  <polygon points="64,40 70,53 86,46 77,60 94,65 78,71 86,84 71,77 65,91 59,76 44,83 52,70 36,64 52,58 45,45 59,52" fill="#f1b969" stroke="#171d1a" stroke-width="3.5" stroke-linejoin="round"/>

  <!-- Stylized Knife-Edge Wrestling Chop Hand Slicing Across -->
  <g>
    <!-- Speed / Slap Motion Lines -->
    <line x1="10" y1="46" x2="28" y2="52" stroke="#f1b969" stroke-width="4.5" stroke-linecap="round"/>
    <line x1="8" y1="80" x2="25" y2="75" stroke="#f1b969" stroke-width="4.5" stroke-linecap="round"/>
    <!-- Chopping Open Hand / Forearm Blade -->
    <path d="M14 90 L52 58 L64 47 C67 44 72 45 73 49 L68 56 L92 43 C96 41 100 45 97 49 L77 62 L103 52 C107 50 110 55 106 59 L81 70 L102 64 C106 63 108 68 104 71 L79 79 L91 78 C95 78 96 83 92 85 L62 92 L24 106 Z" fill="#f4eedc" stroke="#171d1a" stroke-width="4.5" stroke-linejoin="round"/>
    <!-- Crimson Knuckle / Edge Highlight -->
    <path d="M30 88 L64 62" stroke="#b42e25" stroke-width="4" stroke-linecap="round"/>
  </g>
</svg>
```

### 2. Color Tokens & Tactile Button System

| Token | Value | Role |
|---|---|---|
| `--bg-canvas` | `#101713` | Deep forest-charcoal background on `/`, `/demo/`, `/account`, `/control` |
| `--bg-panel` | `#1a241e` | Elevated ledger panel surface on dark pages |
| `--bg-inset` | `#131b16` | Form inputs, code blocks, chat preview boxes |
| `--bg-receipt` | `#f4eedc` | Parchment paper surface for `<Overlay />` and active receipt previews |
| `--ink-light` | `#f4eedc` | Primary headline/body text on dark surfaces |
| `--ink-muted` | `#d4d0c5` | Secondary copy on dark surfaces |
| `--ink-note` | `#b9b3a5` | Helper notes, borders and dividers |
| `--ink-dark` | `#171d1a` | Ink text and borders on parchment receipt cards |
| `--accent-gold` | `#f1b969` | Primary CTA fill, eyebrows, active indicators |
| `--accent-crimson` | `#b42e25` | `RECEIPT.` stamp border/ink, `btn-stamp` fill, danger zone border |
| `--shadow-hard` | `8px 8px 0 #080b09` | Panel offset shadow (`3px 3px 0 #080b09` on buttons) |

**Button Variants:**
- **`.btn-primary`:** Background `#f1b969`, text `#101713`, border `2px solid #080b09`, `font-weight: 800`, `box-shadow: 3px 3px 0 #080b09`.
- **`.btn-stamp`:** Background `#b42e25`, text `#f4eedc`, border `2px solid #080b09`, `font-weight: 800`, `box-shadow: 3px 3px 0 #080b09`. Used for **Approve** (stamp on stream) and **Admit**.
- **`.btn-secondary`:** Background `#16201a`, text `#f4eedc`, border `2px solid #b9b3a5`, `font-weight: 700`, `box-shadow: 3px 3px 0 #080b09`.
- **`.btn-danger`:** Background `#2a1413`, text `#f28b82`, border `2px solid #b42e25`, `font-weight: 700`, `box-shadow: 3px 3px 0 #080b09`.
- **Interactions:** Hover lifts `translate(-1px, -1px)` with `4px 4px 0 #080b09`; active press snaps to `translate(2px, 2px)` with `1px 1px 0 #080b09`; `:focus-visible` shows `3px solid #f4eedc` outline (`3px` offset); `:disabled` sets `opacity: 0.55`, flat shadow, `cursor: not-allowed`.

---

## Complete Before -> After UI Text Specification

### 1. Broadcast Overlay (`Overlay.svelte` & `/obs`)

| Location / State | Current Text (Before) | Target Text (After) | Test / Contract Notes |
|---|---|---|---|
| **Top bar brand** | `THE STREAM REMEMBERS` | `THE STREAM REMEMBERS` | Unchanged |
| **Top bar receipt ID** | `#{display.receipt.id}` | `RECEIPT #{shortId(display.receipt.id)}` (with full `display.receipt.id` preserved in `title` or sr-only span so exact-text check in `overlay-checks.ts` passes) | Keeps `overlay-checks.ts` long-ID check passing without visual clutter |
| **Viewer credit (present)** | `Caught by @{display.receipt.clipped_by}` | `Caught by @{display.receipt.clipped_by}` | Unchanged |
| **Viewer credit (missing)** | `Viewer credit unavailable` | `Caught by chat` | Cleaner broadcast fallback |
| **Appeal header** | `CHAT IS THE JURY` / `Appeal in progress` | `CHAT IS THE JURY` / `Appeal in progress` | Preserves `Appeal in progress` for `overlay-checks.ts` |
| **Appeal instructions** | `Vote in chat: !uphold or !overturn` | `Type !uphold or !overturn in chat` | Clear viewer CTA |
| **Verdict labels** | `Admitted` / `Upheld` / `Overturned` / `Context changed` / `Inconclusive` / `Awaiting a decision` | `Promise Broken (Admitted)` / `Upheld by Chat` / `Overturned by Chat` / `Context Changed` / `Inconclusive` / `Awaiting Decision` | `Upheld by Chat` retains `'Upheld'` substring checked by `overlay-checks.ts` |
| **Verdict subtext** | `The verdict is in.` | `The verdict is on the record.` | Matches `ON THE RECORD` eyebrow |
| **Clip status: Pending** | `CLIP PENDING` | `CLIP PENDING · TRANSCRIPT ON FILE` | Retains `CLIP PENDING` substring |
| **Clip status: Loading** | `LOADING CLIP` | `LOADING TWITCH CLIP…` | Clearer state |
| **Clip status: Loaded** | `CLIP FRAME LOADED · VERIFY PLAYBACK` | `CLIP FRAME LOADED · VERIFY PLAYBACK` | Retained for `overlay-checks.ts` and T4.3 truth-in-playback contract |
| **Clip status: Unavailable** | `CLIP EMBED UNAVAILABLE` | `CLIP EMBED UNAVAILABLE · TRANSCRIPT ON FILE` | Retains `CLIP EMBED UNAVAILABLE` substring |
| **Fallback quote & note** | `<p>“{display.receipt.claim}”</p><small>Saved claim · the clip may omit words; transcript evidence remains authoritative.</small>` | Omit duplicate `<p>“{display.receipt.claim}”</p>` when `url && !clipFailed` (the claim is already displayed in the left column); keep `<small>Saved claim from live transcript · transcript evidence remains authoritative if clip timing shifts.</small>` | Retains `Saved claim` and `transcript evidence remains authoritative` substrings for `overlay-checks.ts` |
| **OBS Pairing Card (`/obs`)** | `Checking pairing…` / `Enter this code in your Receipts setup.` | Add `<p class="eyebrow">RECEIPTS · OBS OVERLAY</p>` + `ReceiptMark` above status text | Self-explanatory inside OBS preview |

### 2. Root Landing Page (`/`)

| Element | Current Text (Before) | Target Text (After) |
|---|---|---|
| **When `?fixture=...` is set** | Renders `<Overlay {display} />` on transparent body | Unchanged: renders `<Overlay {display} />` on transparent body with zero chrome |
| **When visited at `/` (no `?fixture`)** | Blank screen with `Try the recorded demo` link and `Set up Receipts` button | Full Dark Forest Ledger landing page (keep uppercase `'RECEIPT'` out of the initial idle SSR body or update `page.test.ts` so the idle-overlay check targets `Overlay` directly) |
| **Eyebrow** | *(None)* | `LIVE STREAM ACCOUNTABILITY · BUILT WITH CODEX` |
| **Headline (`<h1>`)** | *(None)* | `The stream remembers what you said.` |
| **Lead paragraph** | *(None)* | `Streamers make promises on air all the time. Chat types !receipt to lock in the claim. When the streamer goes back on their word, the receipt lands on stream, and chat is the jury.` |
| **Primary CTA** | `Try the recorded demo` | `Try the recorded demo` (`No login · instant replay`) |
| **Secondary CTA** | `Set up Receipts` | `Set up Receipts` (`Streamer sign-in & OBS setup`) |
| **3-Step Strip** | *(None)* | **1. Chat Takes the Receipt**: `!receipt` clips the stream and extracts the spoken promise.<br>**2. The Contradiction Lands**: Live speech-to-text flags a possible walkback privately. The streamer reviews the evidence and approves the stamp before it appears on the overlay.<br>**3. Chat Is the Jury**: Streamer Admits, Appeals to `!uphold` / `!overturn`, or logs Context Changed; `!lore` keeps the permanent score. |
| **Interactive Preview Switcher** | *(None)* | Stage buttons: `Stamp` · `Evidence` · `Appeal` · `Verdict` driving an embedded `<Overlay />` preview |

### 3. Recorded Judge Demo (`/demo/`)

| Element | Current Text (Before) | Target Text (After) |
|---|---|---|
| **Eyebrow** | `RECORDED DEMO · NO LOGIN · NO PROVIDER CALLS` | `RECORDED DEMO · NO LOGIN · NO PROVIDER CALLS` *(preserved for `demo/page.test.ts`)* |
| **Headline** | `The stream remembers what you said.` | `The stream remembers what you said.` |
| **Intro** | `Try one recorded promise through capture, contradiction detection, an appeal vote, and lore. This demo uses isolated, disposable state for your browser.` | Unchanged |
| **Action buttons** | `Replay scenario` (`Replaying…`) / `Reset demo` | `Replay scenario` (`Replaying…`) / `Reset demo` *(preserved for `demo/page.test.ts`)* |
| **Ready status** | `Ready. Visitor data expires automatically.` | `Ready to replay. Visitor sandbox expires automatically.` *(preserves `expires automatically`)* |
| **Live Overlay Stage Tabs** | *(None: `<Overlay />` was not mounted)* | `1. Stamp` · `2. Evidence` · `3. Appeal Vote` · `4. Verdict` rendering `<Overlay />` with the replayed receipt |
| **Timeline steps** | Raw strings (`receipt captured from recorded transcript`, etc.) | Structured timeline:<br>• **1. Promise Captured (`00:16`)**: `@regular` ran `!receipt` on *“I will never take that shortcut again.”*<br>• **2. Contradiction Caught (`00:20`)**: Broadcaster said *“Fine, I'll take the shortcut.”*<br>• **3. Appeal Opened (`00:20`)**: Broadcaster appealed the stamp to Twitch chat.<br>• **4. Jury Verdict (`01:21`)**: Chat voted `!uphold` (1-0); receipt recorded as **Upheld**. |
| **Outcome badges** | `Recorded clip result: unavailable`<br>`Recorded verdict: upheld` | **Evidence Source:** `Transcript Fallback (offline demo, no Twitch clip call)` (`clip_status: unavailable`)<br>**Final Verdict:** `Upheld by Chat (Promise Broken)` (`verdict: upheld`) |
| **Scenario footnote** | `Scenario: “I will never take that shortcut again.” The recorded broadcaster later takes the shortcut; chat upholds the appeal.` | `Recorded scenario: The broadcaster swears “I will never take that shortcut again,” caves four seconds later, and loses the chat appeal.` |

### 4. Account & OBS Setup (`/account`)

| State / Element | Current Text (Before) | Target Text (After) |
|---|---|---|
| **Signed-out prompt** | `Connect your Twitch account to set up Receipts.` | `Sign in with your Twitch broadcaster account to pair OBS and manage live sessions.` |
| **Status: `unavailable`** | `Listening is unavailable.` | `Live listening is not configured on this server.` |
| **Status: `unknown`** | `Stream status is unknown. Try again.` | `Could not verify stream status. Retrying automatically…` |
| **Status: `offline`** | `Your channel is offline.` | `Offline · Go live on Twitch to start a Receipts session.` |
| **Status: `missing_grant`** | `Reconnect Twitch to grant the required permissions.` | `Missing Twitch permissions · Reconnect to grant chat and clip access.` |
| **Status: `capacity`** | `Listening capacity is full. Try again later.` | `Server capacity reached · All active channel slots are currently in use.` |
| **Status: `not_listening`** | `Your channel is live and not listening.` | `Channel is LIVE · Ready to start listening.` |
| **Status: `session_active`** | `Live session active. Audio capture is not attached.` | `Live session active · Waiting for broadcast audio stream.` |
| **Status: `listening`** | `Listening.` | `Listening · Manual review enabled.` |
| **Status: `paused`** | `Listening; stamps paused.` | `Listening · Overlay stamps paused.` |
| **Sole-speaker active note** | `Transcription enabled for your sole voice.` | `Solo broadcast confirmed · Speech-to-text attribution is active.` |
| **Sole-speaker off note** | `Audio is being monitored; transcription is off because sole-speaker confirmation was not given.` | `Speech-to-text is paused for this session because sole-speaker confirmation was not checked.` |
| **Sole-speaker helper** | `Leave this unchecked if other voices may be heard. Microphone capture is unavailable.` | `Only check this when you are the sole voice on stream. Leave unchecked during co-streams, interviews, or voice chat.` |
| **OBS URL helper** | `Add this URL as a browser source, then enter the code shown there.` | `Add this URL as an OBS Browser Source (1920 × 1080), then enter the pairing code displayed in OBS.` + **[ Copy URL ]** (`Copied!`) button |
| **Paired source item** | `Source added {toLocaleDateString()}` | `Source #{id.slice(0, 6)} · added {toLocaleString()}` + **[ Revoke ]** |
| **Danger zone** | `Sign out` `Disconnect Twitch` `Delete account` inline | **Session:** `[ Sign out ]`<br>**Danger Zone:** `[ Disconnect Twitch ]` · `[ Delete account ]` |

### 5. Streamer Control Dock (`/control`)

| Section / Element | Current Text (Before) | Target Text (After) |
|---|---|---|
| **Header eyebrow & title** | `PRIVATE DOCK` / `Receipts` | `STREAMER CONTROL DOCK` / `Receipts` |
| **Status grid** | `Live: Confirmed / Not confirmed`<br>`Audio: Listening / not listening`<br>`Stamps: On / Paused / Unavailable` | `Stream: Confirmed / Offline / Not confirmed / Unknown` (derived from verified status + `health.live_confirmed`, preserving `Unknown` when stale)<br>`Speech-to-Text: Active / Off`<br>`Stamps: Manual review enabled / Paused / Unavailable` |
| **Start session controls** | `[ Start session ]` (without sole-speaker checkbox) | Sole-speaker confirmation checkbox + `[ Start session ]` (sends `{ sole_broadcaster }` to `/account-api/start`) |
| **Current stamp empty** | `No stamp awaiting an answer.` | `No active stamp on screen.` |
| **Context changed label** | `Why did the context change?` | `What changed? (Recorded on the ledger)` |
| **Quiet review intro** | `These candidates are private until approved.` | `Review-only mode: detected contradictions wait here privately. Approve to slam the stamp onto your stream, or Reject to dismiss.` |
| **Quiet review item** | `<p class="claim">{item.claim}</p><p>{item.reason}</p>` (`reason` hardcoded to `uncertain contradiction`) | **THE PROMISE:** `“{item.claim}”`<br>**HEARD NOW:** `“{item.reason}”` (populated from `detections.evidence_text`)<br>Buttons: `[ Approve ]` (`.btn-stamp`) · `[ Reject ]` (`.btn-secondary`) |
| **Void section** | `Void a receipt` (manual ID input only) | **Channel Ledger & Void**: lists active receipts from `snapshot.receipts` with verdict badge, `@clipped_by` and one-click `[ Void ]` button, plus the manual `Receipt ID` input form (`#void-id` + `[ Void receipt ]`) |
| **Retry footer note** | `If an answer fails, retry the same choice and reason. The retry identity survives a refresh in this tab.` | `If a connection drop interrupts an action, click the same choice again; safe retries survive tab refreshes.` |

### 6. Twitch Chat Replies (`internal/twitch/chat/router.go` & `internal/lore/lore.go`)

| Command | Current Behavior / Text (Before) | Target Behavior / Text (After) |
|---|---|---|
| **`!receipt` (on `ReplyCaptured`)** | Silent: no chat message sent | Sends rate-limited chat message via `r.Sender`: `🧾 Receipt taken (@<login>): "<claim>"` (bounded to 450 UTF-8 bytes; silent on duplicate/throttled/no_claim/suppressed/unavailable) |
| **`!lore`** | `Lore: 1 total | 0 open, 1 broken, 0 overturned, 0 context changed, 0 inconclusive. Claims: Never take the shortcut again.` | `Lore: 1 total | 0 open, 1 broken, 0 overturned, 0 context changed, 0 inconclusive. Claims: "Never take the shortcut again." (broken)` (annotates each listed claim with `(open)`, `(broken)`, `(overturned)`, `(context changed)` or `(inconclusive)` within the 450-byte UTF-8 ceiling) |

---

## Exit criteria

- [x] Every task above (`TU.1`-`TU.4`) has evidence against its done criteria.
- [x] `./tools/check.sh` passes on the candidate revision without keys, network or live broadcasts.
- [x] Mounted browser checks (`overlay-checks.ts`) pass at `1920×1080` and `640×360` including `prefers-reduced-motion`.
- [x] All contract and test updates are documented in the handoff log below.

## Handoff log

### What exists now

TU.1 through TU.4 are implemented and independently reviewed. The full offline gate and targeted race checks passed.

### Evidence and decisions

- **TU.1:** `codex/tu1-brand-shell`; added the exact Knife-Edge Chop SVG component and favicon, shared dark-ledger tokens, accessible tactile button/status classes, and a responsive human-route shell. `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passed. Browser UI verification was unavailable in the subagent session; SvelteKit diagnostics, the production build, and all 29 overlay tests passed. `/obs` and fixture routes are excluded from the shell by the root layout.

### Notes for the next task

`TU.1` is unblocked immediately. `TU.2` and `TU.3` depend on `TU.1`, and `TU.4` closes the phase before `T6.1`.

Parent TU.1 review confirmed the shared home navigation in the mounted browser, transparent body/html and absent navigation on the root fixture route, and absent navigation on OBS. Full combined offline gate passed. Detailed responsive route verification continues with TU.2/TU.3.

### TU.4: Twitch chat feedback

Implemented `!receipt` confirmation on captured receipts only, with per-user and global send limits and a 450-byte UTF-8 bound. Added outcome tags to `!lore` claims while retaining its scoreboard prefix and byte ceiling. Wired the existing appeal service, lore service and verified connection adapter sender into the production EventSub router; this composition wiring was necessary for chat replies to reach their existing ports. Added offline router tests and extended the synthetic EventSub acceptance sink to assert the capture confirmation. `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passed, as did targeted race tests for chat, lore and EventSub. Parent mounted browser review covered home, demo, account, control and chrome-free OBS/fixture routes. Prior combined overlay lifecycle, responsive and reduced-motion evidence is recorded in [TU.2/TU.3 review](../docs/evidence/tu2-tu3-browser.md). No Twitch send or paid provider call was made. See [TU.4 chat feedback evidence](../docs/evidence/tu4-chat-feedback.md).
