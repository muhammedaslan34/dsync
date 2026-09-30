// App state (paired computers, messages kept in memory, pending sends and
// downloads) and the actions that change it. A tiny external store read with
// useSyncExternalStore, so work keeps going when screens change.

import { useSyncExternalStore } from 'react';
import { Directory, File, Paths } from 'expo-file-system';
import * as Sharing from 'expo-sharing';
import {
  type Computer,
  type Message,
  AppError,
  DsyncError,
  PhoneClient,
  describeError,
  isDsyncError,
} from './dsync';
import { safeFileName } from './format';
import * as storage from './storage';
import nacl from 'tweetnacl';
import { AppState } from 'react-native';
import { ensureHost, hostForget, hostSendFile, hostSendText, loadHostedConversations, setHostBridge } from './host';

export const ONLINE_WINDOW_MS = 6000;
const SYNC_PAGE = 200;

export interface Thread {
  messages: Message[]; // by id, oldest first
  maxId: number; // highest id seen from /sync
  loaded: boolean;
  lastOk: number; // when a sync last succeeded (0: never)
  error: DsyncError | null; // last sync error
  addrIdx: number; // which address to try next
}

export interface Pending {
  key: string;
  kind: 'text' | 'file';
  transferId?: string; // files: lets the computer keep what arrived, to resume
  text?: string;
  uri?: string;
  name?: string;
  size: number;
  sent: number;
  status: 'sending' | 'failed';
  error?: unknown; // shown with describeError, so it follows the language
  time: number;
}

export interface Download {
  busy: boolean;
  wanted?: boolean; // started and not finished: resumes when the computer is reachable
  name?: string;
  got: number;
  size: number;
  error?: unknown; // shown with describeError
  uri?: string; // cached file, once complete
}

export interface State {
  ready: boolean;
  computers: Computer[];
  threads: Record<string, Thread>;
  pending: Record<string, Pending[]>;
  downloads: Record<string, Download>;
}

let state: State = { ready: false, computers: [], threads: {}, pending: {}, downloads: {} };
const listeners = new Set<() => void>();

function setState(fn: (s: State) => State) {
  state = fn(state);
  listeners.forEach((l) => l());
}

export function getState(): State {
  return state;
}

export function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useStore<T>(select: (s: State) => T): T {
  return useSyncExternalStore(subscribe, () => select(state));
}

export const NO_PENDING: Pending[] = [];

export const EMPTY_THREAD: Thread = { messages: [], maxId: 0, loaded: false, lastOk: 0, error: null, addrIdx: 0 };

export function threadOf(s: State, cid: string): Thread {
  return s.threads[cid] ?? EMPTY_THREAD;
}

export function isOnline(t: Thread, now: number): boolean {
  return t.lastOk > 0 && now - t.lastOk < ONLINE_WINDOW_MS;
}

function patchThread(cid: string, fn: (t: Thread) => Partial<Thread>) {
  setState((s) => {
    const t = threadOf(s, cid);
    return { ...s, threads: { ...s.threads, [cid]: { ...t, ...fn(t) } } };
  });
}

function mergeMessages(list: Message[], add: Message[]): Message[] {
  if (add.length === 0) return list;
  const byId = new Map<number, Message>();
  for (const m of list) byId.set(m.id, m);
  for (const m of add) if (m && typeof m.id === 'number') byId.set(m.id, m);
  return Array.from(byId.values()).sort((a, b) => a.id - b.id);
}

// ---------- computers ----------

export async function loadComputers() {
  const computers = await storage.loadComputers();
  setState((s) => ({ ...s, ready: true, computers }));
  loadHostedConversations();
  restoreTransfers();
}

// Phones that paired with this one by scanning its code (see host.ts).
setHostBridge({
  computers: () => state.computers,
  addPeer: async (c) => {
    await addComputer(c);
  },
  removePeer: (id) => forgetComputer(id, true),
  changed: (id, messages, lastSeen) => {
    if (!getComputer(id)) return;
    patchThread(id, (t) => ({
      messages,
      maxId: messages.length ? messages[messages.length - 1].id : 0,
      loaded: true,
      lastOk: lastSeen || t.lastOk,
      error: null,
    }));
  },
});

export function getComputer(cid: string): Computer | undefined {
  return state.computers.find((c) => c.id === cid);
}

async function updateComputer(c: Computer) {
  setState((s) => ({ ...s, computers: s.computers.map((x) => (x.id === c.id ? c : x)) }));
  try {
    await storage.saveComputer(c);
  } catch {
    // keep going with the in-memory copy
  }
}

/** Adds a newly paired computer (replacing an older pairing with the same computer). */
export async function addComputer(c: Computer) {
  await storage.saveComputer(c);
  setState((s) => {
    const threads = { ...s.threads };
    delete threads[c.id]; // new phoneId: start the conversation fresh
    return { ...s, computers: [...s.computers.filter((x) => x.id !== c.id), c], threads };
  });
}

/**
 * Tells the computer to forget this phone, then removes it here. With
 * force, removes it here even if the computer can't be reached.
 */
export async function forgetComputer(cid: string, force = false) {
  const c = getComputer(cid);
  if (!c) return;
  if (c.hosted) {
    // It connects to this phone: it finds out it's forgotten on its next request.
    hostForget(cid);
  } else if (!force) {
    try {
      await new PhoneClient(c).unpair();
    } catch (e) {
      // 401: the computer already forgot this phone.
      if (!isDsyncError(e, 'unauthorized')) throw e;
    }
  }
  await storage.removeComputer(cid);
  setState((s) => {
    const threads = { ...s.threads };
    const pending = { ...s.pending };
    delete threads[cid];
    delete pending[cid];
    return { ...s, computers: s.computers.filter((x) => x.id !== cid), threads, pending };
  });
  for (const [k, d] of wantedDownloads) if (d.cid === cid) wantedDownloads.delete(k);
  saveTransfers();
}

function candidates(c: Computer): string[] {
  return [c.address, ...c.addresses.filter((a) => a !== c.address)];
}

/** A client on the address that works (or the one to try next). */
function clientFor(c: Computer): PhoneClient {
  const addrs = candidates(c);
  const t = threadOf(state, c.id);
  return new PhoneClient(c, addrs[t.addrIdx % addrs.length]);
}

// ---------- sync ----------

const inflight = new Set<string>();

/** One /sync round for a computer; fetches every page of new messages. */
export async function pollOnce(cid: string) {
  const c = getComputer(cid);
  if (!c || c.hosted || inflight.has(cid)) return; // hosted: it polls this phone instead
  inflight.add(cid);
  const client = clientFor(c);
  try {
    let since = threadOf(state, cid).maxId;
    const got: Message[] = [];
    let info: { id: string; name: string; os: string } | undefined;
    for (let page = 0; page < 50; page++) {
      const r = await client.sync(since);
      info = r.computer;
      const msgs = Array.isArray(r.messages) ? r.messages : [];
      got.push(...msgs);
      for (const m of msgs) if (m.id > since) since = m.id;
      if (msgs.length < SYNC_PAGE) break;
    }
    if (!getComputer(cid)) return; // forgotten meanwhile
    patchThread(cid, (t) => ({
      messages: mergeMessages(t.messages, got),
      maxId: Math.max(t.maxId, since),
      loaded: true,
      lastOk: Date.now(),
      error: null,
      addrIdx: 0,
    }));
    resumeTransfers(cid);
    const cur = getComputer(cid)!;
    const changed =
      cur.address !== client.address ||
      (info && info.name && info.name !== cur.name) ||
      (info && info.os && info.os !== cur.os);
    if (changed) {
      await updateComputer({ ...cur, address: client.address, name: info?.name || cur.name, os: info?.os || cur.os });
    }
  } catch (e) {
    const err = e instanceof DsyncError ? e : new DsyncError('server', describeError(e));
    patchThread(cid, (t) => ({
      error: err,
      // Unreachable: try the computer's next address next time.
      addrIdx: err.kind === 'unreachable' ? t.addrIdx + 1 : t.addrIdx,
    }));
  } finally {
    inflight.delete(cid);
  }
}

// ---------- sending ----------

let seq = 0;

function patchPending(cid: string, key: string, patch: Partial<Pending> | null) {
  setState((s) => {
    const list = s.pending[cid] ?? [];
    const next = patch === null ? list.filter((p) => p.key !== key) : list.map((p) => (p.key === key ? { ...p, ...patch } : p));
    return { ...s, pending: { ...s.pending, [cid]: next } };
  });
}

function addPending(cid: string, p: Pending) {
  setState((s) => ({ ...s, pending: { ...s.pending, [cid]: [...(s.pending[cid] ?? []), p] } }));
}

function delivered(cid: string, key: string, m: Message | undefined) {
  patchPending(cid, key, null);
  if (m && typeof m.id === 'number') patchThread(cid, (t) => ({ messages: mergeMessages(t.messages, [m]) }));
}

const running = new Set<string>();

async function run(cid: string, key: string) {
  const c = getComputer(cid);
  const p = (state.pending[cid] ?? []).find((x) => x.key === key);
  if (!c || !p || running.has(key)) return;
  running.add(key);
  patchPending(cid, key, { status: 'sending', error: undefined });
  const client = clientFor(c);
  try {
    if (p.kind === 'text') {
      delivered(cid, key, await client.sendText(p.text ?? ''));
    } else {
      let last = -1;
      const m = await client.upload({ uri: p.uri!, name: p.name!, transferId: p.transferId }, (sent, size) => {
        // Don't re-render more often than needed.
        if (last < 0 || sent === size || Math.abs(sent - last) >= size / 100) {
          last = sent;
          patchPending(cid, key, { sent, size });
        }
      });
      delivered(cid, key, m);
    }
  } catch (e) {
    patchPending(cid, key, { status: 'failed', error: e });
  } finally {
    running.delete(key);
    saveTransfers();
  }
}

function newTransferId(): string {
  return Array.from(nacl.randomBytes(16), (b) => b.toString(16).padStart(2, '0')).join('');
}

export function sendText(cid: string, text: string) {
  if (getComputer(cid)?.hosted) return hostSendText(cid, text);
  const key = `p${++seq}`;
  addPending(cid, { key, kind: 'text', text, size: text.length, sent: 0, status: 'sending', time: Date.now() });
  void run(cid, key);
}

export function sendFile(cid: string, file: { uri: string; name: string; size?: number }) {
  if (getComputer(cid)?.hosted) return hostSendFile(cid, file);
  const key = `p${++seq}`;
  let size = file.size ?? 0;
  try {
    size = new File(file.uri).size || size;
  } catch {
    // keep the picker's size
  }
  addPending(cid, {
    key,
    kind: 'file',
    transferId: newTransferId(),
    uri: file.uri,
    name: file.name,
    size,
    sent: 0,
    status: 'sending',
    time: Date.now(),
  });
  saveTransfers();
  void run(cid, key);
}

export function retryPending(cid: string, key: string) {
  void run(cid, key);
}

export function dismissPending(cid: string, key: string) {
  patchPending(cid, key, null);
  saveTransfers();
}

// ---------- downloads ----------

export function downloadKey(cid: string, id: number): string {
  return `${cid}:${id}`;
}

function patchDownload(k: string, patch: Partial<Download>) {
  setState((s) => ({
    ...s,
    downloads: { ...s.downloads, [k]: { ...(s.downloads[k] ?? { busy: false, got: 0, size: 0 }), ...patch } },
  }));
}

/** The file a download is saved to (and resumed from). */
function downloadFile(cid: string, m: Message): File {
  const dir = new Directory(Paths.cache, 'dsync', `${cid.replace(/[^A-Za-z0-9._-]/g, '_')}-${m.id}`);
  dir.create({ intermediates: true, idempotent: true });
  return new File(dir, safeFileName(m.file!.name));
}

/** Downloads (or finishes downloading) a file the computer sent; true once it's all there. */
async function fetchFile(cid: string, m: Message): Promise<File | null> {
  const c = getComputer(cid);
  if (!c || !m.file) return null;
  const k = downloadKey(cid, m.id);
  if (state.downloads[k]?.busy) return null;
  const dest = downloadFile(cid, m);
  if (dest.exists && dest.size === m.file.size) {
    patchDownload(k, { busy: false, wanted: false, uri: dest.uri, got: m.file.size, error: undefined });
    return dest;
  }
  patchDownload(k, { busy: true, wanted: true, name: m.file.name, got: dest.exists ? dest.size ?? 0 : 0, size: m.file.size, error: undefined, uri: undefined });
  wantedDownloads.set(k, { cid, id: m.id, name: m.file.name, size: m.file.size });
  saveTransfers();
  try {
    let last = -1;
    await clientFor(c).download(
      m.id,
      dest,
      (got, size) => {
        if (last < 0 || got >= size || got - last >= size / 100) {
          last = got;
          patchDownload(k, { got, size });
        }
      },
      m.file.size,
    );
    patchDownload(k, { busy: false, wanted: false, uri: dest.uri, got: m.file.size });
    wantedDownloads.delete(k);
    saveTransfers();
    return dest;
  } catch (e) {
    patchDownload(k, { busy: false, error: e });
    return null;
  }
}

/** Downloads a file the computer sent (if not already here) and opens the share sheet. */
export async function saveOrShare(cid: string, m: Message) {
  if (!m.file) return;
  const k = downloadKey(cid, m.id);
  // From a hosted phone the file is already here.
  const dest = m.file.uri ? new File(m.file.uri) : await fetchFile(cid, m);
  if (!dest) return;
  // Finished while the app was in the background: it waits for a tap instead.
  if (AppState.currentState !== 'active') return;
  try {
    if (!(await Sharing.isAvailableAsync())) throw new AppError('errors.noSharing', 'Sharing is not available on this phone');
    await Sharing.shareAsync(dest.uri, { dialogTitle: m.file.name });
  } catch (e) {
    patchDownload(k, { error: e });
  }
}

// ---------- unfinished transfers survive the app being closed ----------

interface SavedTransfers {
  v: 1;
  pending: { cid: string; key: string; transferId?: string; uri: string; name: string; size: number; time: number }[];
  downloads: { cid: string; id: number; name: string; size: number }[];
}

const wantedDownloads = new Map<string, { cid: string; id: number; name: string; size: number }>();

function transfersFile(): File {
  return new File(Paths.document, 'dsync-transfers.json');
}

/** Remembers unfinished file sends and downloads (not their progress: the computer and the file on disk know that). */
function saveTransfers() {
  const data: SavedTransfers = { v: 1, pending: [], downloads: [...wantedDownloads.values()] };
  for (const [cid, list] of Object.entries(state.pending)) {
    for (const p of list) {
      if (p.kind === 'file' && p.uri && p.name) {
        data.pending.push({ cid, key: p.key, transferId: p.transferId, uri: p.uri, name: p.name, size: p.size, time: p.time });
      }
    }
  }
  try {
    const f = transfersFile();
    if (data.pending.length === 0 && data.downloads.length === 0) {
      if (f.exists) f.delete();
      return;
    }
    f.write(JSON.stringify(data));
  } catch {
    // not saved: they still finish while the app stays open
  }
}

/** At startup: puts back the transfers that were going when the app closed, and continues them. */
function restoreTransfers() {
  let data: SavedTransfers | null = null;
  try {
    const f = transfersFile();
    if (f.exists) data = JSON.parse(f.textSync()) as SavedTransfers;
  } catch {
    data = null;
  }
  if (!data || data.v !== 1) return;
  for (const p of data.pending ?? []) {
    if (!getComputer(p.cid)) continue;
    const key = `p${++seq}`;
    addPending(p.cid, {
      key,
      kind: 'file',
      transferId: p.transferId,
      uri: p.uri,
      name: p.name,
      size: p.size,
      sent: 0,
      status: 'sending',
      time: p.time,
    });
    void run(p.cid, key);
  }
  for (const d of data.downloads ?? []) {
    if (!getComputer(d.cid)) continue;
    const k = downloadKey(d.cid, d.id);
    wantedDownloads.set(k, d);
    patchDownload(k, { busy: false, wanted: true, size: d.size });
    void fetchFile(d.cid, { id: d.id, time: 0, fromPhone: false, file: { name: d.name, size: d.size, status: 'done' } });
  }
  saveTransfers();
}

/** Continues what stopped because the computer couldn't be reached, now that it answers. */
function resumeTransfers(cid: string) {
  for (const p of state.pending[cid] ?? []) {
    if (p.kind === 'file' && p.status === 'failed' && p.transferId && isDsyncError(p.error, 'unreachable')) void run(cid, p.key);
  }
  for (const d of wantedDownloads.values()) {
    const cur = state.downloads[downloadKey(d.cid, d.id)];
    if (d.cid === cid && cur && !cur.busy && (cur.error === undefined || isDsyncError(cur.error, 'unreachable'))) {
      void fetchFile(d.cid, { id: d.id, time: 0, fromPhone: false, file: { name: d.name, size: d.size, status: 'done' } });
    }
  }
}
