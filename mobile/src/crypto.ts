// Sealed envelopes for the dsync phone protocol (docs/phone-protocol.md):
//   base64( nonce[24] || secretbox(envelope_json, nonce, key) )
// with envelope_json = {"t": <unix ms>, "b": <body>}.
//
// This file only depends on tweetnacl so it can be tested with plain Node.
// The app seeds tweetnacl's PRNG from expo-crypto in random.ts.

import nacl from 'tweetnacl';

export const KEY_SIZE = 32;
export const NONCE_SIZE = 24;
/** Envelopes older than this (or this far in the future) are refused. */
export const MAX_AGE_MS = 2 * 60 * 1000;

// ---------- base64 ----------

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
const B64_LOOKUP: Int16Array = (() => {
  const t = new Int16Array(128).fill(-1);
  for (let i = 0; i < B64.length; i++) t[B64.charCodeAt(i)] = i;
  // base64url too
  t['-'.charCodeAt(0)] = 62;
  t['_'.charCodeAt(0)] = 63;
  return t;
})();

/** Standard base64 with padding. */
export function toBase64(bytes: Uint8Array): string {
  const parts: string[] = [];
  let chunk = '';
  const n = bytes.length;
  let i = 0;
  for (; i + 2 < n; i += 3) {
    const v = (bytes[i] << 16) | (bytes[i + 1] << 8) | bytes[i + 2];
    chunk += B64[v >> 18] + B64[(v >> 12) & 63] + B64[(v >> 6) & 63] + B64[v & 63];
    if (chunk.length >= 8192) {
      parts.push(chunk);
      chunk = '';
    }
  }
  if (i < n) {
    const b1 = i + 1 < n ? bytes[i + 1] : 0;
    const v = (bytes[i] << 16) | (b1 << 8);
    chunk += B64[v >> 18] + B64[(v >> 12) & 63];
    chunk += i + 1 < n ? B64[(v >> 6) & 63] + '=' : '==';
  }
  parts.push(chunk);
  return parts.join('');
}

/** Decodes standard base64 or base64url, with or without padding. Throws on bad input. */
export function fromBase64(s: string): Uint8Array {
  let str = s.replace(/[\s=]+/g, '');
  if (str.length % 4 === 1) throw new Error('bad base64');
  const out = new Uint8Array(Math.floor((str.length * 3) / 4));
  let o = 0;
  let acc = 0;
  let bits = 0;
  for (let i = 0; i < str.length; i++) {
    const c = str.charCodeAt(i);
    const v = c < 128 ? B64_LOOKUP[c] : -1;
    if (v < 0) throw new Error('bad base64');
    acc = (acc << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[o++] = (acc >> bits) & 0xff;
    }
  }
  return o === out.length ? out : out.subarray(0, o);
}

// ---------- UTF-8 ----------

export function utf8Encode(s: string): Uint8Array {
  const out: number[] = [];
  for (let i = 0; i < s.length; i++) {
    let c = s.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
      const d = s.charCodeAt(i + 1);
      if (d >= 0xdc00 && d <= 0xdfff) {
        c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00);
        i++;
      }
    }
    if (c >= 0xd800 && c <= 0xdfff) c = 0xfffd; // lone surrogate
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
  }
  return Uint8Array.from(out);
}

export function utf8Decode(b: Uint8Array): string {
  const parts: string[] = [];
  let codes: number[] = [];
  const flush = () => {
    parts.push(String.fromCharCode.apply(null, codes));
    codes = [];
  };
  let i = 0;
  while (i < b.length) {
    const c = b[i];
    let cp = 0xfffd;
    let len = 1;
    if (c < 0x80) cp = c;
    else if (c >= 0xc2 && c < 0xe0 && i + 1 < b.length && (b[i + 1] & 0xc0) === 0x80) {
      cp = ((c & 31) << 6) | (b[i + 1] & 63);
      len = 2;
    } else if (c >= 0xe0 && c < 0xf0 && i + 2 < b.length && (b[i + 1] & 0xc0) === 0x80 && (b[i + 2] & 0xc0) === 0x80) {
      cp = ((c & 15) << 12) | ((b[i + 1] & 63) << 6) | (b[i + 2] & 63);
      len = 3;
      if (cp < 0x800 || (cp >= 0xd800 && cp <= 0xdfff)) cp = 0xfffd;
    } else if (
      c >= 0xf0 && c < 0xf5 && i + 3 < b.length &&
      (b[i + 1] & 0xc0) === 0x80 && (b[i + 2] & 0xc0) === 0x80 && (b[i + 3] & 0xc0) === 0x80
    ) {
      cp = ((c & 7) << 18) | ((b[i + 1] & 63) << 12) | ((b[i + 2] & 63) << 6) | (b[i + 3] & 63);
      len = 4;
      if (cp < 0x10000 || cp > 0x10ffff) cp = 0xfffd;
    }
    i += len;
    if (cp >= 0x10000) {
      cp -= 0x10000;
      codes.push(0xd800 + (cp >> 10), 0xdc00 + (cp & 1023));
    } else codes.push(cp);
    if (codes.length > 4096) flush();
  }
  flush();
  return parts.join('');
}

// ---------- secretbox ----------

/** base64(nonce || secretbox(data, nonce, key)). The nonce is random unless given (tests). */
export function sealBytes(key: Uint8Array, data: Uint8Array, nonce?: Uint8Array): string {
  if (key.length !== KEY_SIZE) throw new Error('bad key');
  const n = nonce ?? nacl.randomBytes(NONCE_SIZE);
  if (n.length !== NONCE_SIZE) throw new Error('bad nonce');
  const box = nacl.secretbox(data, n, key);
  const out = new Uint8Array(NONCE_SIZE + box.length);
  out.set(n, 0);
  out.set(box, NONCE_SIZE);
  return toBase64(out);
}

/** Opens what sealBytes produced; returns null if it doesn't open. */
export function openBytes(key: Uint8Array, sealed: string): Uint8Array | null {
  let raw: Uint8Array;
  try {
    raw = fromBase64(sealed.trim());
  } catch {
    return null;
  }
  if (raw.length < NONCE_SIZE + nacl.secretbox.overheadLength) return null;
  return nacl.secretbox.open(raw.subarray(NONCE_SIZE), raw.subarray(0, NONCE_SIZE), key);
}

// ---------- envelopes ----------

export class EnvelopeError extends Error {
  kind: 'open' | 'stale';
  constructor(kind: 'open' | 'stale', message: string) {
    super(message);
    this.kind = kind;
  }
}

/** Seals {"t": now, "b": body}. */
export function sealEnvelope(key: Uint8Array, body: unknown, now: number = Date.now(), nonce?: Uint8Array): string {
  return sealBytes(key, utf8Encode(JSON.stringify({ t: now, b: body })), nonce);
}

/** Opens an envelope and returns its body, refusing ones older than 2 minutes (or that far in the future). */
export function openEnvelope<T = unknown>(key: Uint8Array, sealed: string, now: number = Date.now()): T {
  const data = openBytes(key, sealed);
  if (!data) throw new EnvelopeError('open', 'The reply could not be decrypted');
  let env: { t?: unknown; b?: unknown };
  try {
    env = JSON.parse(utf8Decode(data));
  } catch {
    throw new EnvelopeError('open', 'The reply was not valid');
  }
  if (typeof env !== 'object' || env === null || typeof env.t !== 'number') {
    throw new EnvelopeError('open', 'The reply was not valid');
  }
  if (Math.abs(now - env.t) > MAX_AGE_MS) {
    throw new EnvelopeError('stale', "The phone's and computer's clocks are too far apart");
  }
  return env.b as T;
}
