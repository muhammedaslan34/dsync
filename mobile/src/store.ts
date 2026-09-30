// App state (paired computers, messages kept in memory, pending sends and
// downloads) and the actions that change it. A tiny external store read with
// useSyncExternalStore, so work keeps going when screens change.

import { useSyncExternalStore } from 'react';
import { Directory, File, Paths } from 'expo-file-system';
import * as Sharing from 'expo-sharing';
import {
  type Computer,
  type Message,
  DsyncError,
  PhoneClient,
  describeError,
  isDsyncError,
} from './dsync';
import { safeFileName } from './format';
import * as storage from './storage';

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
  text?: string;
  uri?: string;
  name?: string;
  size: number;
  sent: number;
  status: 'sending' | 'failed';
  error?: string;
  time: number;
}

export interface Download {
  busy: boolean;
  got: number;
  size: number;
  error?: string;
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

function subscribe(l: () => void) {
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
}

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
  if (!force) {
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
  if (!c || inflight.has(cid)) return;
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

async function run(cid: string, key: string) {
  const c = getComputer(cid);
  const p = (state.pending[cid] ?? []).find((x) => x.key === key);
  if (!c || !p) return;
  patchPending(cid, key, { status: 'sending', error: undefined, sent: 0 });
  const client = clientFor(c);
  try {
    if (p.kind === 'text') {
      delivered(cid, key, await client.sendText(p.text ?? ''));
    } else {
      let last = 0;
      const m = await client.upload({ uri: p.uri!, name: p.name! }, (sent, size) => {
        // Don't re-render more often than needed.
        if (sent === size || sent - last >= size / 100) {
          last = sent;
          patchPending(cid, key, { sent, size });
        }
      });
      delivered(cid, key, m);
    }
  } catch (e) {
    patchPending(cid, key, { status: 'failed', error: describeError(e) });
  }
}

export function sendText(cid: string, text: string) {
  const key = `p${++seq}`;
  addPending(cid, { key, kind: 'text', text, size: text.length, sent: 0, status: 'sending', time: Date.now() });
  void run(cid, key);
}

export function sendFile(cid: string, file: { uri: string; name: string; size?: number }) {
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
    uri: file.uri,
    name: file.name,
    size,
    sent: 0,
    status: 'sending',
    time: Date.now(),
  });
  void run(cid, key);
}

export function retryPending(cid: string, key: string) {
  void run(cid, key);
}

export function dismissPending(cid: string, key: string) {
  patchPending(cid, key, null);
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

/** Downloads a file the computer sent (if not already cached) and opens the share sheet. */
export async function saveOrShare(cid: string, m: Message) {
  const c = getComputer(cid);
  if (!c || !m.file) return;
  const k = downloadKey(cid, m.id);
  const d = state.downloads[k];
  if (d?.busy) return;
  const dir = new Directory(Paths.cache, 'dsync', `${cid.replace(/[^A-Za-z0-9._-]/g, '_')}-${m.id}`);
  const dest = new File(dir, safeFileName(m.file.name));
  try {
    const cached = d?.uri && dest.exists && dest.size === m.file.size;
    if (!cached) {
      patchDownload(k, { busy: true, got: 0, size: m.file.size, error: undefined, uri: undefined });
      dir.create({ intermediates: true, idempotent: true });
      let last = 0;
      await clientFor(c).download(m.id, dest, (got, size) => {
        if (got >= size || got - last >= size / 100) {
          last = got;
          patchDownload(k, { got, size });
        }
      });
      patchDownload(k, { busy: false, uri: dest.uri, got: m.file.size });
    }
    if (!(await Sharing.isAvailableAsync())) throw new Error('Sharing is not available on this phone');
    await Sharing.shareAsync(dest.uri, { dialogTitle: m.file.name });
  } catch (e) {
    patchDownload(k, { busy: false, error: describeError(e) });
  }
}
