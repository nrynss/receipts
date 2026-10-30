import { mount, unmount, flushSync } from 'svelte';
import Harness from './Harness.svelte';
import { fixture } from '../fixtures';
import { displayFromSnapshot } from '../model';
import type { Display } from '../model';

const result = document.querySelector('#result')!;
const target = document.querySelector('#target') as HTMLElement;
const checks: string[] = [];
// Intercept only the component's clip deadline. This keeps the check fast and
// lets us assert that replacement and unmount cancel outstanding deadlines.
const nativeTimeout = window.setTimeout.bind(window);
const nativeClearTimeout = window.clearTimeout.bind(window);
const deadlines = new Map<number, () => void>();
let nextDeadline = -1;
window.setTimeout = ((handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
  if (timeout === 8000 && typeof handler === 'function') { const id = nextDeadline--; deadlines.set(id, () => handler(...args)); return id; }
  return nativeTimeout(handler, timeout, ...args);
}) as typeof window.setTimeout;
window.clearTimeout = ((id?: number) => { if (id !== undefined && deadlines.delete(id)) return; nativeClearTimeout(id); }) as typeof window.clearTimeout;

function assert(ok: unknown, message: string) { if (!ok) throw new Error(message); checks.push(message); }
function show(display: Display) { const component = mount(Harness, { target, props: { initial: display } }); flushSync(); return component; }
function text() { return target.textContent || ''; }
async function remove(component: ReturnType<typeof show>) { await unmount(component); flushSync(); }
async function run() {
  let component = show(fixture('evidence'));
  assert(text().includes('CLIP EMBED UNAVAILABLE') && text().includes('Saved claim') && text().includes('transcript evidence remains authoritative'), 'unavailable clip shows the exact saved claim and transcript fallback');
  await remove(component);
  const pending = fixture('evidence'); pending.receipt = { ...pending.receipt!, clip_status: 'pending' };
  component = show(pending); assert(text().includes('CLIP PENDING'), 'pending clip shows saved claim'); await remove(component);
  const ready = fixture('evidence'); ready.receipt = { ...ready.receipt!, clip_status: 'ready', clip_url: 'https://clips.twitch.tv/ControlledFixture' };
  component = show(ready); flushSync();
  let frame = target.querySelector('iframe'); assert(frame && text().includes('Saved claim'), 'ready iframe retains saved claim');
  frame!.dispatchEvent(new Event('error')); flushSync();
  assert(!target.querySelector('iframe') && text().includes('CLIP EMBED UNAVAILABLE') && text().includes('Saved claim'), 'iframe error falls back without hiding the saved claim'); await remove(component);
  component = show(ready); flushSync(); frame = target.querySelector('iframe');
  assert(frame, 'ready iframe mounted for timeout');
  assert(deadlines.size === 1, 'clip deadline scheduled');
  const deadline = [...deadlines.values()][0]; deadline(); flushSync();
  assert(!target.querySelector('iframe') && text().includes('CLIP EMBED UNAVAILABLE') && text().includes('Saved claim'), 'iframe timeout falls back without hiding the saved claim'); await remove(component);
  component = show(ready); flushSync(); frame = target.querySelector('iframe');
  assert(frame?.getAttribute('allow') === 'autoplay; fullscreen' && frame.src.includes('autoplay=false'), 'OBS clip playback stays click-to-play');
  frame!.dispatchEvent(new Event('load')); flushSync();
  assert(text().includes('CLIP FRAME LOADED · VERIFY PLAYBACK'), 'iframe load is described as frame load, not playback proof'); await remove(component);
  const snapshot = displayFromSnapshot({ epoch:'test', channel:'demo', live:true, receipts:[ready.receipt!], current_stamp:ready.receipt, sequence:1 });
  component = show(snapshot); assert(!target.querySelector('.arrive'), 'snapshot does not animate'); await remove(component);
  component = show(fixture('stamp')); assert(target.querySelector('.arrive'), 'fresh stamp animates'); await remove(component);
  component = show(fixture('stamp'));
  for (const state of ['evidence','appeal','verdict','idle'] as const) { component.setDisplay(fixture(state)); flushSync(); assert(state === 'idle' ? !target.querySelector('.receipt') : text().includes(state === 'appeal' ? 'Appeal in progress' : state === 'verdict' ? 'Upheld' : 'CLIP EMBED UNAVAILABLE'), `transition to ${state}`); }
  await remove(component);
  const long = fixture('evidence'); long.receipt = { ...long.receipt!, claim:'Exact long claim '.repeat(80), id:'id'.repeat(100), clipped_by:'viewer'.repeat(60) };
  component = show(long); const card = target.querySelector('.receipt') as HTMLElement;
  assert(text().includes(long.receipt.claim) && text().includes(long.receipt.clipped_by!), 'long text remains exact');
  assert(card.scrollWidth <= card.clientWidth && card.getBoundingClientRect().height <= innerHeight * .86, 'long text stays within viewport without horizontal overflow'); await remove(component);
  assert(!target.querySelector('.receipt') && deadlines.size === 0, 'unmount removes overlay and cancels clip deadline');
  if (matchMedia('(prefers-reduced-motion: reduce)').matches) { component = show(fixture('stamp')); const card = target.querySelector('.receipt')!; assert(getComputedStyle(card).animationName === 'none', 'reduced motion disables stamp animation'); await remove(component); }
  result.textContent = `PASS: ${checks.join(' | ')}`;
}
run().catch(error => { result.textContent = `FAIL: ${error.message}; passed: ${checks.join(' | ')}`; });
