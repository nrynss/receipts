"""Score frozen recordings with the owner's post-collection label audit."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
def read(path):
    return json.loads((ROOT / path).read_text())
def digest(path):
    return hashlib.sha256((ROOT / path).read_bytes()).hexdigest()
def ratio(n, d):
    return n / d if d else None

review_path = 'docs/t54-label-review-progress.json'
review = read(review_path)
dataset_path = 'testdata/eval/pairs.jsonl'
assert review['complete'] and digest(dataset_path) == review['dataset_sha256']
pairs = [json.loads(line) for line in (ROOT / dataset_path).read_text().splitlines()]
labels = {p['id']: dict(p) for p in pairs}
corrections = {p['id']: p for p in review['disagreements']}
assert set(review['approved_ids']).isdisjoint(corrections)
assert set(review['approved_ids']) | set(corrections) == set(labels)
for key, correction in corrections.items():
    assert labels[key]['label'] == correction['original_label']
    assert labels[key]['expected'] == correction['original_expected']
    labels[key]['label'] = correction['reviewed_label']
    labels[key]['expected'] = correction['reviewed_expected']
reports = []
for split in ('dev', 'heldout'):
    path = f'docs/evidence/t54-{split}-recording.json'
    bundle = read(path)
    assert bundle['dataset_sha256'] == review['dataset_sha256']
    assert bundle['run_status'] == 'complete'
    expected = {p['id'] for p in pairs if p['split'] == split}
    assert {r['id'] for r in bundle['sessions']} == expected
    counts = dict(cases=len(expected), eligible_contradictions=0, negative_labels=0,
                  target_stamps=0, correct_stamps=0, false_stamps=0, abstentions=0,
                  retrieval_attempts=0, retrieval_misses=0, source_rejections=0,
                  calls=0, spend_nano_usd=0, latency_ms=0)
    false_ids = []
    for case in bundle['sessions']:
        assert not case.get('error_stage')
        events = case['trace']['events']
        assert all(e['measured'] and e['state'] == 'complete' and not e.get('failed') for e in events)
        counts['calls'] += len(events)
        counts['spend_nano_usd'] += sum(e['spend_nano_usd'] for e in events)
        counts['latency_ms'] += case['latency_ms']
        p = labels[case['id']]
        positive = p['label'] == 'contradiction' and p['expected'] == 'stamp'
        counts['eligible_contradictions' if positive else 'negative_labels'] += 1
        result = case['result']
        outcome = result['outcomes'].get(case['id'])
        if outcome == 'stamp':
            counts['target_stamps'] += 1
            counts['correct_stamps' if positive else 'false_stamps'] += 1
            if not positive:
                false_ids.append(case['id'])
        elif outcome != 'dismissed':
            counts['abstentions'] += 1
        if result['source_rejected']:
            counts['source_rejections'] += 1
        elif case['id'] in (result['captured'] or []):
            counts['retrieval_attempts'] += 1
            counts['retrieval_misses'] += not result['target_retrieved']
    reports.append(dict(split=split, recording_sha256=digest(path), counts=counts,
        precision=ratio(counts['correct_stamps'], counts['target_stamps']),
        recall=ratio(counts['correct_stamps'], counts['eligible_contradictions']),
        false_stamp_rate=ratio(counts['false_stamps'], counts['negative_labels']),
        abstention_rate=ratio(counts['abstentions'], counts['cases']),
        retrieval_miss_rate=ratio(counts['retrieval_misses'], counts['retrieval_attempts']),
        false_stamp_ids=false_ids))
print(json.dumps(dict(protocol=review['protocol'], dataset_sha256=review['dataset_sha256'],
    review_sha256=digest(review_path), label_corrections=len(corrections),
    policy='review-only-v1', automatic_stamps_enabled=False,
    limitation='Post-collection audit of small synthetic fixtures; no claim of real-world accuracy. Zero false stamps with zero stamps is not evidence of useful stamp precision.',
    partitions=reports), indent=2))
