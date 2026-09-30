// Phone to phone. The phone that shows a code runs a small server that
// speaks the same protocol as dsync on a computer (docs/phone-protocol.md),
// so the phone that scans it talks to it exactly like to a computer. Here the
// other phone is a `hosted` peer: its conversation lives on this phone, what
// this phone sends waits until the other phone's next sync picks it up, and
// files it sends land in this app's documents.
//
// Both apps must be open (on Android a running transfer keeps the app alive,
// see background.ts).

import { AppState, Platform } from 'react-native';
import TcpSocket from 'react-native-tcp-socket';
import * as Device from 'expo-device';
import * as Network from 'expo-network';
import nacl from 'tweetnacl';
import { Directory, File, FileMode, Paths } from 'expo-file-system';
import { fromBase64, openEnvelope, sealEnvelope, toBase64, utf8Encode, KEY_SIZE } from './crypto';
import type { Computer, Message } from './dsync';
import { safeFileName } from './format';
import * as storage from './storage';

export const HOST_PORT = 47103;
const PAIRING_TTL = 10 * 60 * 1000;
const CHUNK = 512 * 1024;
const MAX_READ = 1 << 20;
const MAX_BODY = 3 << 20;
const SYNC_LIMIT = 200;
// An upload with no chunk for this long is shown as interrupted (it resumes when the other phone is back).
const UPLOAD_IDLE = 60 * 1000;

// ---------- hooks into the app's store (set by store.ts, avoids an import cycle) ----------

export interface HostBridge {
  computers(): Computer[];
  addPeer(c: Computer): Promise<void>;
  removePeer(id: string): Promise<void>;
  /** The conversation with a hosted peer changed (or it was just heard from). */
  changed(id: string, messages: Message[], lastSeen: number): void;
}

let bridge: HostBridge | null = null;

export function setHostBridge(b: HostBridge) {
  bridge = b;
}

// ---------- this phone ----------

let deviceId = '';

export function deviceName(): string {
  return Device.deviceName || Device.modelName || (Platform.OS === 'ios' ? 'iPhone' : 'Android phone');
}

// ---------- conversations with hosted peers ----------

interface HostFile {
  name: string;
  size: number;
  status: string;
  error?: string;
  uri?: string; // the file on this phone (sent: the picked file; received: once complete)
  received?: number;
  transferId?: string;
}

interface HostMsg {
  id: number;
  time: number;
  mine: boolean; // sent by this phone
  text?: string;
  file?: HostFile;
}

interface History {
  nextId: number;
  messages: HostMsg[];
}

const histories = new Map<string, History>();
const lastSeen = new Map<string, number>();

function dataDir(): Directory {
  const d = new Directory(Paths.document, 'dsync');
  d.create({ intermediates: true, idempotent: true });
  return d;
}

function safeId(id: string): string {
  return id.replace(/[^A-Za-z0-9._-]/g, '_');
}

function historyFile(peer: string): File {
  return new File(dataDir(), `peer-${safeId(peer)}.json`);
}

function history(peer: string): History {
  let h = histories.get(peer);
  if (!h) {
    h = { nextId: 1, messages: [] };
    try {
      const f = historyFile(peer);
      if (f.exists) {
        const saved = JSON.parse(f.textSync()) as History;
        if (saved && Array.isArray(saved.messages)) h = saved;
      }
    } catch {}
    // Anything that was arriving when the app closed stopped there.
    for (const m of h.messages) {
      if (m.file && m.file.status === 'active') m.file = { ...m.file, status: 'failed', error: 'interrupted' };
    }
    histories.set(peer, h);
  }
  return h;
}

function save(peer: string) {
  try {
    historyFile(peer).write(JSON.stringify(history(peer)));
  } catch {}
}

/** Messages as this phone's UI sees them (fromPhone: sent by this phone). */
function asMessages(peer: string): Message[] {
  return history(peer).messages.map((m) => ({
    id: m.id,
    time: m.time,
    fromPhone: m.mine,
    text: m.text,
    file: m.file ? { name: m.file.name, size: m.file.size, status: m.file.status, error: m.file.error, uri: m.file.uri, received: m.file.received } : undefined,
  }));
}

function notify(peer: string) {
  bridge?.changed(peer, asMessages(peer), lastSeen.get(peer) ?? 0);
}

function add(peer: string, m: Omit<HostMsg, 'id' | 'time'>): HostMsg {
  const h = history(peer);
  const msg: HostMsg = { ...m, id: h.nextId++, time: Date.now() };
  h.messages.push(msg);
  if (h.messages.length > 2000) h.messages = h.messages.slice(-2000);
  save(peer);
  notify(peer);
  return msg;
}

function patchFile(peer: string, id: number, patch: Partial<HostFile>, persist = true) {
  const m = history(peer).messages.find((x) => x.id === id);
  if (!m?.file) return;
  m.file = { ...m.file, ...patch };
  if (persist) save(peer);
  notify(peer);
}

/** Loads the conversations with hosted peers into the store; call after the peers are loaded. */
export function loadHostedConversations() {
  for (const c of bridge?.computers() ?? []) if (c.hosted) notify(c.id);
}

/** Sends text to a hosted peer (it picks it up on its next sync). */
export function hostSendText(peer: string, text: string) {
  add(peer, { mine: true, text });
}

/** Offers a file to a hosted peer; it downloads it from this phone. */
export function hostSendFile(peer: string, file: { uri: string; name: string; size?: number }) {
  let size = file.size ?? 0;
  try {
    size = new File(file.uri).size || size;
  } catch {}
  add(peer, { mine: true, file: { name: file.name, size, status: 'done', uri: file.uri } });
}

/** Forgets a hosted peer's conversation (and the files it sent stay in documents). */
export function hostForget(peer: string) {
  histories.delete(peer);
  lastSeen.delete(peer);
  try {
    const f = historyFile(peer);
    if (f.exists) f.delete();
  } catch {}
}

// ---------- pairing ----------

export interface HostPairing {
  url: string;
  expires: number;
}

let pairing: { pid: string; key: Uint8Array; expires: number } | null = null;
const pairedListeners = new Set<(peerId: string) => void>();

/** Called with the new peer's id when a phone pairs with the code on screen. */
export function onPhonePaired(l: (peerId: string) => void) {
  pairedListeners.add(l);
  return () => pairedListeners.delete(l);
}

function hex(b: Uint8Array): string {
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

function base64url(b: Uint8Array): string {
  return toBase64(b).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

/** Makes a one-time code for another phone to scan (and starts the server). Null when there's no Wi-Fi address. */
export async function startHostPairing(): Promise<HostPairing | null> {
  await ensureHost();
  if (!server) throw new Error('could not listen');
  let ip = '';
  try {
    ip = await Network.getIpAddressAsync();
  } catch {}
  if (!ip || ip === '0.0.0.0') return null;
  const key = nacl.randomBytes(KEY_SIZE);
  const pid = hex(nacl.randomBytes(12));
  pairing = { pid, key, expires: Date.now() + PAIRING_TTL };
  const q = [
    ['v', '1'],
    ['pid', pid],
    ['key', base64url(key)],
    ['name', deviceName()],
    ['cid', deviceId],
    ['os', Platform.OS === 'ios' ? 'ios' : 'android'],
    ['port', String(HOST_PORT)],
    ['addr', ip],
  ]
    .map(([k, v]) => `${k}=${encodeURIComponent(v)}`)
    .join('&');
  return { url: `dsync://pair?${q}`, expires: pairing.expires };
}

export function stopHostPairing() {
  pairing = null;
}

// ---------- the server ----------

type Server = ReturnType<typeof TcpSocket.createServer>;
interface Socket {
  on(event: 'data' | 'error', cb: (d: unknown) => void): unknown;
  write(data: string, encoding: 'ascii', cb?: (err?: Error) => void): unknown;
  end(): unknown;
  destroy(): unknown;
}

let server: Server | null = null;
let idleTimer: ReturnType<typeof setInterval> | null = null;
let starting: Promise<void> | null = null;

/** Starts the server if it isn't running (after startup, and when the app comes back). */
export function ensureHost(): Promise<void> {
  if (server) return Promise.resolve();
  if (starting) return starting;
  starting = (async () => {
    if (!deviceId) deviceId = await storage.loadDeviceId();
    await new Promise<void>((resolve) => {
      const s = TcpSocket.createServer((socket) => serveConnection(socket as unknown as Socket));
      s.on('error', () => {
        if (server === s) server = null;
        resolve();
      });
      s.on('close', () => {
        if (server === s) server = null;
      });
      s.listen({ port: HOST_PORT, host: '0.0.0.0', reuseAddress: true }, () => {
        server = s;
        resolve();
      });
      idleTimer ??= setInterval(dropIdleUploads, 20 * 1000);
    });
  })().finally(() => {
    starting = null;
  });
  return starting;
}

/** Runs the server whenever there are hosted peers; call once at startup. */
export async function startHost() {
  deviceId = await storage.loadDeviceId();
  const want = () => pairing !== null || (bridge?.computers() ?? []).some((c) => c.hosted);
  if (want()) void ensureHost();
  AppState.addEventListener('change', (s) => {
    // iOS may close the listening socket while the app is in the background.
    if (s === 'active' && want()) void ensureHost();
  });
}

interface Request {
  path: string;
  headers: Record<string, string>;
  body: string;
}

function serveConnection(socket: Socket) {
  const chunks: Uint8Array[] = [];
  let total = 0;
  let need = -1;
  let head: { path: string; headers: Record<string, string>; bodyStart: number } | null = null;
  let done = false;
  socket.on('error', () => {});
  socket.on('data', (d: unknown) => {
    if (done) return;
    const b = typeof d === 'string' ? utf8Encode(d) : (d as Uint8Array);
    chunks.push(b);
    total += b.length;
    const all = () => {
      const out = new Uint8Array(total);
      let o = 0;
      for (const c of chunks) {
        out.set(c, o);
        o += c.length;
      }
      chunks.length = 0;
      chunks.push(out);
      return out;
    };
    if (!head) {
      const buf = all();
      const end = indexOfCRLF2(buf);
      if (end < 0) {
        if (total > 16384) finish(socket, 431, 'headers too large');
        return;
      }
      const lines = latin1(buf.subarray(0, end)).split('\r\n');
      const [method, path] = lines[0].split(' ');
      const headers: Record<string, string> = {};
      for (const l of lines.slice(1)) {
        const i = l.indexOf(':');
        if (i > 0) headers[l.slice(0, i).trim().toLowerCase()] = l.slice(i + 1).trim();
      }
      const len = Number(headers['content-length'] ?? '0');
      if (method !== 'POST') return stop(405, 'POST only');
      if (!Number.isFinite(len) || len < 0 || len > MAX_BODY) return stop(413, 'too big');
      head = { path: path ?? '', headers, bodyStart: end + 4 };
      need = head.bodyStart + len;
    }
    if (total < need) return;
    const buf = all();
    done = true;
    const req: Request = { path: head.path, headers: head.headers, body: latin1(buf.subarray(head.bodyStart, need)) };
    void handle(req).then(
      (r) => finish(socket, r.status, r.body),
      () => finish(socket, 500, 'internal error'),
    );
  });
  function stop(status: number, msg: string) {
    done = true;
    finish(socket, status, msg);
  }
}

function indexOfCRLF2(b: Uint8Array): number {
  for (let i = 0; i + 3 < b.length; i++) {
    if (b[i] === 13 && b[i + 1] === 10 && b[i + 2] === 13 && b[i + 3] === 10) return i;
  }
  return -1;
}

function latin1(b: Uint8Array): string {
  let s = '';
  for (let i = 0; i < b.length; i += 8192) s += String.fromCharCode(...b.subarray(i, i + 8192));
  return s;
}

const REASONS: Record<number, string> = { 200: 'OK', 400: 'Bad Request', 401: 'Unauthorized', 404: 'Not Found', 405: 'Method Not Allowed', 410: 'Gone', 413: 'Payload Too Large', 431: 'Request Header Fields Too Large', 500: 'Internal Server Error', 507: 'Insufficient Storage' };

/** Writes the reply and closes. Bodies are ASCII: sealed (base64) or short English errors. */
function finish(socket: Socket, status: number, body: string) {
  const ascii = body.replace(/[^\x20-\x7e]/g, '?');
  const head = `HTTP/1.1 ${status} ${REASONS[status] ?? 'Error'}\r\nContent-Type: text/plain\r\nContent-Length: ${ascii.length}\r\nConnection: close\r\n\r\n`;
  try {
    // Close only once the reply is out (the write happens on another thread).
    socket.write(head + ascii, 'ascii', () => {
      try {
        socket.end();
      } catch {}
    });
  } catch {
    try {
      socket.destroy();
    } catch {}
  }
}

// ---------- requests ----------

type Reply = { status: number; body: string };
const nonces = new Map<string, number>();

function openRequest<T>(key: Uint8Array, body: string): T | null {
  const now = Date.now();
  for (const [k, t] of nonces) if (now - t > 3 * 60 * 1000) nonces.delete(k);
  const nonce = body.trim().slice(0, 32);
  if (nonces.has(nonce)) return null;
  let v: T;
  try {
    v = openEnvelope<T>(key, body.trim());
  } catch {
    return null;
  }
  nonces.set(nonce, now);
  return v;
}

const sealed = (key: Uint8Array, v: unknown): Reply => ({ status: 200, body: sealEnvelope(key, v) });
const err = (status: number, msg: string): Reply => ({ status, body: msg });

async function handle(req: Request): Promise<Reply> {
  if (req.path === '/phone/v1/pair') return pair(req);
  const m = /^\/phone\/v1\/([a-z/]+)$/.exec(req.path);
  if (!m) return err(404, 'not found');
  const peer = (bridge?.computers() ?? []).find((c) => c.hosted && c.phoneId === req.headers['x-dsync-phone']);
  let key: Uint8Array | null = null;
  try {
    key = peer ? fromBase64(peer.key) : null;
  } catch {}
  if (!peer || !key || key.length !== KEY_SIZE) return err(401, 'unknown phone; pair again');
  const body = openRequest<Record<string, unknown>>(key, req.body);
  if (body === null) return err(401, 'could not open the request');
  lastSeen.set(peer.id, Date.now());
  const id = peer.id;
  let r: Reply;
  switch (m[1]) {
    case 'sync':
      r = syncReply(id, key, Number(body.since) || 0);
      break;
    case 'text': {
      const text = typeof body.text === 'string' ? body.text : '';
      if (!text.trim()) return err(400, 'empty message');
      if (utf8Encode(text).length > 1 << 20) return err(413, 'message too long; send it as a file');
      const msg = add(id, { mine: false, text });
      r = sealed(key, { message: forPeer(msg) });
      break;
    }
    case 'upload/start':
      r = uploadStart(id, key, body);
      break;
    case 'upload/chunk':
      r = uploadChunk(id, key, body);
      break;
    case 'upload/finish':
      r = uploadFinish(id, key, body);
      break;
    case 'download':
      r = download(id, key, body);
      break;
    case 'unpair':
      await bridge?.removePeer(id);
      return sealed(key, {});
    default:
      return err(404, 'not found');
  }
  notify(id);
  return r;
}

async function pair(req: Request): Promise<Reply> {
  const p = pairing;
  if (!p || p.pid !== req.headers['x-dsync-pair'] || Date.now() > p.expires) {
    return err(410, 'pairing code expired or already used; show a new one on the other phone');
  }
  const body = openRequest<{ name?: string; platform?: string }>(p.key, req.body);
  if (!body) return err(401, 'could not open the request');
  if (pairing !== p) return err(410, 'pairing code already used');
  pairing = null;
  let name = typeof body.name === 'string' ? body.name.trim() : '';
  if (!name || name.length > 64) name = 'Phone';
  const os = body.platform === 'ios' || body.platform === 'android' ? body.platform : 'android';
  const id = 'phone-' + hex(nacl.randomBytes(8));
  const peer: Computer = {
    id,
    name,
    os,
    key: toBase64(p.key),
    phoneId: id,
    addresses: [],
    address: '',
    port: 0,
    pairedAt: Date.now(),
    hosted: true,
  };
  try {
    await bridge?.addPeer(peer);
  } catch {
    return err(500, 'could not save the pairing');
  }
  lastSeen.set(id, Date.now());
  notify(id);
  pairedListeners.forEach((l) => l(id));
  return sealed(p.key, { phoneId: id, computerId: deviceId, computerName: deviceName(), os: Platform.OS === 'ios' ? 'ios' : 'android' });
}

/** A message as the other phone sees it (fromPhone: sent by it). */
function forPeer(m: HostMsg) {
  return {
    id: m.id,
    time: m.time,
    fromPhone: !m.mine,
    text: m.text,
    file: m.file ? { name: m.file.name, size: m.file.size, status: m.file.status, error: m.file.error } : undefined,
  };
}

function syncReply(peer: string, key: Uint8Array, since: number): Reply {
  const messages = history(peer)
    .messages.filter((m) => m.id > since)
    .slice(0, SYNC_LIMIT)
    .map(forPeer);
  return sealed(key, {
    computer: { id: deviceId, name: deviceName(), os: Platform.OS === 'ios' ? 'ios' : 'android' },
    messages,
  });
}

// ---------- files from the other phone ----------

interface Upload {
  peer: string;
  msgId: number;
  name: string;
  size: number;
  received: number;
  part: File;
  resumable: boolean;
  touched: number;
}

const uploads = new Map<string, Upload>();

function partsDir(): Directory {
  const d = new Directory(dataDir(), 'parts');
  d.create({ intermediates: true, idempotent: true });
  return d;
}

function receivedDir(peer: string): Directory {
  const d = new Directory(Paths.document, 'dsync', 'received', safeId(peer));
  d.create({ intermediates: true, idempotent: true });
  return d;
}

function dropIdleUploads() {
  const now = Date.now();
  for (const [id, u] of uploads) {
    if (now - u.touched < UPLOAD_IDLE) continue;
    uploads.delete(id);
    if (!u.resumable) {
      try {
        u.part.delete();
      } catch {}
    }
    patchFile(u.peer, u.msgId, { status: 'failed', error: 'interrupted', received: undefined });
  }
}

function uploadStart(peer: string, key: Uint8Array, body: Record<string, unknown>): Reply {
  const size = Number(body.size);
  const tid = typeof body.transferId === 'string' ? body.transferId : '';
  if (!Number.isInteger(size) || size < 0 || (tid && !/^[0-9a-f]{16,64}$/i.test(tid))) return err(400, 'bad request');
  dropIdleUploads();
  const name = safeFileName(typeof body.name === 'string' ? body.name : 'file');
  if (!tid) {
    const uploadId = hex(nacl.randomBytes(12));
    const part = new File(partsDir(), `${uploadId}.part`);
    part.create({ overwrite: true });
    const msg = add(peer, { mine: false, file: { name, size, status: 'active', received: 0 } });
    uploads.set(`${peer}|${uploadId}`, { peer, msgId: msg.id, name, size, received: 0, part, resumable: false, touched: Date.now() });
    return sealed(key, { uploadId, chunkSize: CHUNK, offset: 0 });
  }
  // Resumable: the same transfer id finds the same .part file (see "Resuming an upload" in the protocol).
  const uploadId = `r${tid.toLowerCase()}`;
  const k = `${peer}|${uploadId}`;
  const open = uploads.get(k);
  if (open && open.size === size) {
    open.touched = Date.now();
    patchFile(peer, open.msgId, { status: 'active', error: undefined });
    return sealed(key, { uploadId, chunkSize: CHUNK, offset: open.received });
  }
  uploads.delete(k);
  const part = new File(partsDir(), `${safeId(peer)}-${uploadId}.part`);
  let have = 0;
  if (part.exists) have = part.size ?? 0;
  if (have > size || !part.exists) {
    part.create({ overwrite: true });
    have = 0;
  }
  const h = history(peer);
  const old = [...h.messages].reverse().find((m) => !m.mine && m.file?.transferId === tid);
  let msgId: number;
  if (old) {
    msgId = old.id;
    patchFile(peer, msgId, { name, size, status: 'active', error: undefined, received: have });
  } else {
    msgId = add(peer, { mine: false, file: { name, size, status: 'active', received: have, transferId: tid } }).id;
  }
  uploads.set(k, { peer, msgId, name, size, received: have, part, resumable: true, touched: Date.now() });
  return sealed(key, { uploadId, chunkSize: CHUNK, offset: have });
}

function uploadChunk(peer: string, key: Uint8Array, body: Record<string, unknown>): Reply {
  const u = uploads.get(`${peer}|${String(body.uploadId)}`);
  if (!u) return err(404, 'no such upload');
  let data: Uint8Array;
  try {
    data = fromBase64(typeof body.data === 'string' ? body.data : '');
  } catch {
    return err(400, 'bad data');
  }
  if (data.length > CHUNK) return err(413, 'chunk too big');
  if (Number(body.offset) !== u.received) return err(400, `expected offset ${u.received}`);
  if (u.received + data.length > u.size) return err(400, "more data than the file's size");
  try {
    const h = u.part.open(FileMode.ReadWrite);
    try {
      h.offset = u.received;
      h.writeBytes(data);
    } finally {
      h.close();
    }
  } catch (e) {
    return err(507, 'could not write the file');
  }
  u.received += data.length;
  u.touched = Date.now();
  patchFile(peer, u.msgId, { received: u.received }, false);
  return sealed(key, { received: u.received });
}

function uploadFinish(peer: string, key: Uint8Array, body: Record<string, unknown>): Reply {
  const k = `${peer}|${String(body.uploadId)}`;
  const u = uploads.get(k);
  if (!u) return err(404, 'no such upload');
  if (u.received !== u.size) return err(400, `only ${u.received} of ${u.size} bytes arrived`);
  uploads.delete(k);
  const dir = receivedDir(peer);
  let dest = new File(dir, u.name);
  for (let i = 1; dest.exists; i++) {
    const dot = u.name.lastIndexOf('.');
    dest = new File(dir, dot > 0 ? `${u.name.slice(0, dot)} (${i})${u.name.slice(dot)}` : `${u.name} (${i})`);
  }
  try {
    u.part.move(dest);
  } catch {
    patchFile(peer, u.msgId, { status: 'failed', error: 'could not save the file', received: undefined });
    return err(500, 'could not save the file');
  }
  patchFile(peer, u.msgId, { status: 'done', uri: dest.uri, received: undefined, error: undefined });
  const m = history(peer).messages.find((x) => x.id === u.msgId)!;
  return sealed(key, { message: forPeer(m) });
}

// ---------- files to the other phone ----------

function download(peer: string, key: Uint8Array, body: Record<string, unknown>): Reply {
  const offset = Number(body.offset);
  const length = Number(body.length);
  if (!Number.isInteger(offset) || offset < 0 || !Number.isInteger(length) || length <= 0) return err(400, 'bad request');
  const m = history(peer).messages.find((x) => x.id === Number(body.messageId) && x.mine && x.file?.uri);
  if (!m?.file?.uri) return err(404, 'no such file');
  const f = new File(m.file.uri);
  if (!f.exists) return err(404, 'the file is no longer there');
  const size = f.size ?? 0;
  let bytes = new Uint8Array(0);
  if (offset < size) {
    const h = f.open(FileMode.ReadOnly);
    try {
      h.offset = offset;
      bytes = h.readBytes(Math.min(length, MAX_READ, size - offset));
    } finally {
      h.close();
    }
  }
  return sealed(key, { data: toBase64(bytes), size, eof: offset + bytes.length >= size });
}
