import { readFile, writeFile } from 'node:fs/promises';

const source = await readFile('../docs/submission.md', 'utf8');
const section = source.split('## Evidence and limits')[1]?.split('\n## ')[0];
if (!section) throw new Error('Could not locate curated evidence table in docs/submission.md');
const rows = section.replaceAll(';', '.').replaceAll('—', ',').split('\n').filter((line) => line.startsWith('|')).slice(2).map((line) => line.replace(/\]\((?!https?:|#)([^)]+)\)/g, (_, target) => `](https://github.com/nrynss/receipts/blob/main/docs/${target})`));
if (rows.length < 4) throw new Error('Curated evidence table looks incomplete');
const currentLimits = [
  'Production combines adjacent raw chunks of about five seconds each into overlapping ASR windows.',
  'A window spans nominally ten seconds and has a twelve-second hard limit.',
  'Raw claims stay disjoint, and durable `transcription_windows` rows reference both audio ranges.',
  'Exact full ASR text and window bounds remain evidence.',
  'Receipts skips identical text from shared overlap.',
  'It rejects gaps, stale ownership, and duplicate raw submissions.',
  'Longer promises or ASR errors can still produce abstentions.',
  'These fixes passed offline checks, but paid live verification remains pending.',
];
const dockStatus = [
  'The complete private control read model polls every three seconds.',
  'It preserves typed reasons and action state, then cancels stale reads.',
  'A delayed fixture showed a new review appear without refresh and preserved the typed reason.',
  'Paid live verification remains pending.',
];
const markdown = `---\ntitle: Verified evidence snapshot\ndescription: Build-time snapshot from the repository judge guide.\n---\n\nThis page imports the curated evidence table from the repository judge guide during every build.\n\n| Capability | Evidence and boundary |\n| --- | --- |\n${rows.join('\n')}\n\n${currentLimits.join(' ')}\n\n${dockStatus.join(' ')}\n\n[Open the source judge guide](https://github.com/nrynss/receipts/blob/main/docs/submission.md).\n`;
await writeFile('src/content/docs/verified-evidence.md', markdown);
