import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import Page from './+page.svelte';
import Overlay from '$lib/overlay/Overlay.svelte';
import { fixture } from '$lib/overlay/fixtures';

describe('overlay', () => {
  it('renders a visible, non-animated default stamp in the landing preview', () => { const body = render(Page, { props: { data: { fixtureMode: false } } }).body; expect(body).toContain('The stream remembers what you said.'); expect(body).toContain('Try the recorded demo'); expect(body).toContain('I will take the long way around every time.'); expect(body).toContain('Recorded preview · sample timestamp 2025-01-01 00:00:00 UTC'); expect(body).toContain('aria-pressed'); expect(body).not.toContain('arrive'); });
  it('renders fixture requests in the transparent overlay harness', () => { const body = render(Page, { props: { data: { fixtureMode: true } } }).body; expect(body).toContain('class="harness '); expect(body).not.toContain('The stream remembers what you said.'); });
  it('shows a bounded recorded vote duration for the appeal fixture', () => { const body = render(Overlay, { props: { display: fixture('appeal'), recordedPreview: true } }).body; expect(body).toContain('Recorded vote · 1m 21s remaining at stage start'); expect(body).not.toContain('2277517714'); });
  it('renders each fixture without exposing private material', () => {
    for (const state of ['stamp', 'evidence', 'appeal', 'verdict'] as const) {
      const body = render(Overlay, { props: { display: fixture(state) } }).body;
      expect(body).toContain('long way around'); expect(body).toContain('@river');
      expect(body).not.toContain('review');
    }
    expect(render(Overlay, { props: { display: fixture('evidence') } }).body).toContain('CLIP EMBED UNAVAILABLE');
  });
});
