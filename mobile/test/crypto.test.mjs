// Checks src/crypto.ts against the test vectors in docs/phone-protocol.md.
// Run with: npm run test:crypto   (plain Node >= 23.6, which strips TypeScript types)
import assert from 'node:assert/strict';
import {
  sealBytes,
  openBytes,
  utf8Encode,
  utf8Decode,
  toBase64,
  fromBase64,
  sealEnvelope,
  openEnvelope,
  EnvelopeError,
} from '../src/crypto.ts';

const key = Uint8Array.from({ length: 32 }, (_, i) => i); // 00 01 .. 1f
const nonce = Uint8Array.from({ length: 24 }, (_, i) => 100 + i); // 64 65 .. 7b

const vectors = [
  {
    plain: '{"t":1790752000000,"b":{"text":"héllo dsync"}}',
    sealed:
      'ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp7Ju5EbL6NXn4buApjwG6t8nmb7esAh/nQgMoWpge0kygOmhSFhoRxYIR4J+x+qfOqwKxddCNYu4CGb3kBIm6g',
  },
  { plain: '', sealed: 'ZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXp79JVy1hlCgePIf7tOIQaTLA==' },
];

let passed = 0;
function test(name, fn) {
  fn();
  passed++;
  console.log('ok  ' + name);
}

for (const [i, v] of vectors.entries()) {
  test(`vector ${i + 1} seals`, () => {
    assert.equal(sealBytes(key, utf8Encode(v.plain), nonce), v.sealed);
  });
  test(`vector ${i + 1} opens`, () => {
    const opened = openBytes(key, v.sealed);
    assert.ok(opened, 'did not open');
    assert.equal(utf8Decode(opened), v.plain);
  });
}

test('vector 1 via sealEnvelope with the same time', () => {
  const s = sealEnvelope(key, { text: 'héllo dsync' }, 1790752000000, nonce);
  assert.equal(s, vectors[0].sealed);
  assert.deepEqual(openEnvelope(key, s, 1790752000000 + 1000), { text: 'héllo dsync' });
});

test('wrong key does not open', () => {
  const bad = key.slice();
  bad[0] ^= 1;
  assert.equal(openBytes(bad, vectors[0].sealed), null);
});

test('tampered box does not open', () => {
  const raw = fromBase64(vectors[0].sealed);
  raw[raw.length - 1] ^= 1;
  assert.equal(openBytes(key, toBase64(raw)), null);
});

test('garbage does not open', () => {
  assert.equal(openBytes(key, 'not base64!!'), null);
  assert.equal(openBytes(key, 'AAAA'), null);
});

test('stale and future envelopes are refused', () => {
  const t = 1790752000000;
  const s = sealEnvelope(key, {}, t);
  assert.throws(() => openEnvelope(key, s, t + 2 * 60 * 1000 + 1), (e) => e instanceof EnvelopeError && e.kind === 'stale');
  assert.throws(() => openEnvelope(key, s, t - 2 * 60 * 1000 - 1), (e) => e instanceof EnvelopeError && e.kind === 'stale');
  assert.deepEqual(openEnvelope(key, s, t + 119000), {});
});

test('random nonces differ', () => {
  const a = sealEnvelope(key, { x: 1 });
  const b = sealEnvelope(key, { x: 1 });
  assert.notEqual(a.slice(0, 32), b.slice(0, 32));
});

test('base64 round trips and matches Buffer', () => {
  for (let n = 0; n < 300; n++) {
    const b = Uint8Array.from({ length: n }, (_, i) => (i * 37 + n) & 255);
    const s = toBase64(b);
    assert.equal(s, Buffer.from(b).toString('base64'));
    assert.deepEqual(fromBase64(s), b);
    assert.deepEqual(fromBase64(Buffer.from(b).toString('base64url')), b);
  }
});

test('utf8 matches Buffer', () => {
  const samples = ['', 'abc', 'héllo', '日本語', 'emoji 😀👍🏽', 'mixed \u0000 ߿ ࠀ ￿'];
  for (const s of samples) {
    assert.deepEqual(utf8Encode(s), Uint8Array.from(Buffer.from(s, 'utf8')));
    assert.equal(utf8Decode(Uint8Array.from(Buffer.from(s, 'utf8'))), s);
  }
});

console.log(`\n${passed} tests passed`);
