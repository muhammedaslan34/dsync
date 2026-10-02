// The dsync phone protocol, v1 (docs/phone-protocol.md): pairing URL parsing,
// sealed requests, and the pair / sync / text / upload / download / unpair calls.

import { File, FileMode } from 'expo-file-system';
import { EnvelopeError, KEY_SIZE, fromBase64, openEnvelope, sealEnvelope, toBase64, utf8Encode } from './crypto';
import { type MsgKey, type Vars, t } from './i18n';
import { isLocalNetworkAddress, localNetworkAddresses } from './network';

export const DEFAULT_PORT = 47102;
export const MAX_TEXT_BYTES = 1 << 20;
export const DOWNLOAD_CHUNK = 1 << 20;

// ---------- types ----------

export interface FileMeta {
  name: string;
  size: number;
  status: 'active' | 'done' | 'failed' | 'canceled' | string;
  error?: string;
  /** Phone-to-phone, on the phone that shows the code: the file on this phone. */
  uri?: string;
  /** Bytes received so far, while it's arriving (phone-to-phone). */
  received?: number;
}

export interface Message {
  id: number;
  time: number;
  fromPhone: boolean;
  text?: string;
  file?: FileMeta;
}

/**
 * A paired computer, as stored on the phone. Another phone is stored the same
 * way: the phone that scanned the code talks to the one that showed it just as
 * it talks to a computer; on the phone that showed the code the other phone is
 * `hosted` (it has no address: it's the one that connects, see host.ts).
 */
export interface Computer {
  id: string;
  name: string;
  os: string;
  /** 32-byte key, standard base64. */
  key: string;
  phoneId: string;
  /** Addresses from the QR code, in the computer's order. */
  addresses: string[];
  /** The address that worked last. */
  address: string;
  port: number;
  pairedAt: number;
  /** A phone that paired with this one by scanning its code (see host.ts). */
  hosted?: boolean;
}

/** Whether a device is a phone (by the os it reported). */
export function isPhoneOS(os: string): boolean {
  return os === 'android' || os === 'ios';
}

export interface PairingCode {
  v: number;
  pid: string;
  key: Uint8Array;
  name: string;
  cid: string;
  os: string;
  port: number;
  addrs: string[];
}

// ---------- errors ----------

export type ErrorKind =
  | 'badCode' // the scanned text isn't a dsync pairing URL
  | 'unreachable' // network error or timeout
  | 'expired' // 410, or 401 while pairing: the code is used, expired or wrong
  | 'unauthorized' // 401 after pairing: the computer doesn't know this phone any more
  | 'badRequest' // 400
  | 'notFound' // 404
  | 'tooBig' // 413
  | 'badReply' // the reply didn't open
  | 'clock' // the reply was too old / from the future
  | 'server'; // anything else

export class DsyncError extends Error {
  kind: ErrorKind;
  status?: number;
  /** A translated message to show instead of `message` (which stays English, for logs). */
  msgKey?: MsgKey;
  vars?: Vars;
  constructor(kind: ErrorKind, message: string, status?: number, msgKey?: MsgKey, vars?: Vars) {
    super(message);
    this.kind = kind;
    this.status = status;
    this.msgKey = msgKey;
    this.vars = vars;
  }
}

/** A local failure (reading a file, a stalled download) with a translated message. */
export class AppError extends Error {
  msgKey: MsgKey;
  vars?: Vars;
  constructor(msgKey: MsgKey, message: string, vars?: Vars) {
    super(message);
    this.msgKey = msgKey;
    this.vars = vars;
  }
}

export function isDsyncError(e: unknown, kind?: ErrorKind): e is DsyncError {
  return e instanceof DsyncError && (kind === undefined || e.kind === kind);
}

/** A short, human message for an error, in the current language. */
export function describeError(e: unknown): string {
  if (e instanceof AppError) return t(e.msgKey, e.vars);
  if (e instanceof DsyncError) {
    switch (e.kind) {
      case 'unreachable':
        return t('errors.unreachable');
      case 'expired':
        return t('errors.expired');
      case 'unauthorized':
        return t('errors.unauthorized');
      case 'clock':
        return t('errors.clock');
      case 'badReply':
        return e.msgKey ? t(e.msgKey, e.vars) : t('errors.badReply');
      default:
        // Other replies carry the computer's own (English) message.
        if (e.msgKey) return t(e.msgKey, e.vars);
        return e.message || t('errors.generic');
    }
  }
  if (e instanceof Error && e.message) return e.message;
  return t('errors.generic');
}

// ---------- pairing URL ----------

function decodeParam(s: string): string {
  try {
    return decodeURIComponent(s.replace(/\+/g, ' '));
  } catch {
    return s;
  }
}

/** Parses dsync://pair?v=1&pid=..&key=..&name=..&cid=..&os=..&port=47102&addr=a,b */
export function parsePairingUrl(raw: string): PairingCode {
  const text = raw.trim();
  const m = /^dsync:\/\/pair\/?\?(.*)$/i.exec(text);
  if (!m) throw new DsyncError('badCode', "That's not a dsync pairing code.", undefined, 'errors.notACode');
  const params: Record<string, string> = {};
  for (const part of m[1].split('#')[0].split('&')) {
    if (!part) continue;
    const eq = part.indexOf('=');
    const k = decodeParam(eq < 0 ? part : part.slice(0, eq));
    params[k] = eq < 0 ? '' : decodeParam(part.slice(eq + 1));
  }
  const bad = (what: string) =>
    new DsyncError('badCode', `The pairing code is incomplete (${what}).`, undefined, 'errors.codeIncomplete', { what });
  const v = Number(params.v ?? '1');
  if (v !== 1) throw new DsyncError('badCode', 'This pairing code is from a newer dsync.', undefined, 'errors.codeNewer');
  if (!params.pid) throw bad('pid');
  let key: Uint8Array;
  try {
    key = fromBase64(params.key ?? '');
  } catch {
    throw bad('key');
  }
  if (key.length !== KEY_SIZE) throw bad('key');
  if (!params.cid) throw bad('cid');
  const port = params.port ? Number(params.port) : DEFAULT_PORT;
  if (!Number.isInteger(port) || port <= 0 || port > 65535) throw bad('port');
  const addrs = localNetworkAddresses((params.addr ?? '').split(',').filter(Boolean));
  if (addrs.length === 0) throw bad('addr');
  return { v, pid: params.pid, key, name: params.name || t('computer.title'), cid: params.cid, os: params.os || '', port, addrs };
}

// ---------- requests ----------

function hostPart(addr: string): string {
  const escaped = addr.replace(/%/g, '%25');
  return escaped.includes(':') && !escaped.startsWith('[') ? `[${escaped}]` : escaped;
}

interface Target {
  key: Uint8Array;
  address: string;
  port: number;
}

async function post<T>(
  t: Target,
  path: string,
  header: Record<string, string>,
  body: unknown,
  timeoutMs: number,
): Promise<T> {
  if (!isLocalNetworkAddress(t.address)) {
    throw new DsyncError('unreachable', 'Refusing a cleartext connection outside the local network');
  }
  const sealed = sealEnvelope(t.key, body);
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), timeoutMs);
  let res: Response;
  let text: string;
  try {
    res = await fetch(`http://${hostPart(t.address)}:${t.port}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'text/plain', ...header },
      body: sealed,
      signal: ctrl.signal,
    });
    text = await res.text();
  } catch {
    throw new DsyncError('unreachable', "Can't reach the computer");
  } finally {
    clearTimeout(timer);
  }
  if (res.status !== 200) {
    const msg = text.trim().slice(0, 300) || `HTTP ${res.status}`;
    switch (res.status) {
      case 400:
        throw new DsyncError('badRequest', msg, 400);
      case 401:
        throw new DsyncError('unauthorized', msg, 401);
      case 404:
        throw new DsyncError('notFound', msg, 404);
      case 410:
        throw new DsyncError('expired', msg, 410);
      case 413:
        throw new DsyncError('tooBig', msg, 413);
      default:
        throw new DsyncError('server', msg, res.status);
    }
  }
  try {
    return openEnvelope<T>(t.key, text);
  } catch (e) {
    if (e instanceof EnvelopeError && e.kind === 'stale') throw new DsyncError('clock', e.message);
    throw new DsyncError('badReply', e instanceof Error ? e.message : 'Bad reply');
  }
}

// ---------- pairing ----------

interface PairResponse {
  phoneId: string;
  computerId: string;
  computerName: string;
  os: string;
}

/**
 * Pairs with the computer from a scanned code, trying each address in order
 * (short timeout each) and keeping the first that answers.
 */
export async function pair(
  code: PairingCode,
  phone: { name: string; platform: 'ios' | 'android' },
  onTry?: (address: string) => void,
): Promise<Computer> {
  let lastErr: unknown = new DsyncError('unreachable', "Can't reach the computer");
  for (const address of code.addrs) {
    onTry?.(address);
    try {
      const r = await post<PairResponse>(
        { key: code.key, address, port: code.port },
        '/phone/v1/pair',
        { 'X-Dsync-Pair': code.pid },
        { name: phone.name, platform: phone.platform },
        4000,
      );
      if (!r || typeof r.phoneId !== 'string' || !r.phoneId) throw new DsyncError('badReply', 'The computer sent an unexpected reply', undefined, 'errors.unexpectedReply');
      return {
        id: r.computerId || code.cid,
        name: r.computerName || code.name,
        os: r.os || code.os,
        key: toBase64(code.key),
        phoneId: r.phoneId,
        addresses: code.addrs,
        address,
        port: code.port,
        pairedAt: Date.now(),
      };
    } catch (e) {
      // While pairing a 401 means the body didn't open: the key/code is wrong.
      if (isDsyncError(e, 'unauthorized')) throw new DsyncError('expired', e.message, 401);
      if (isDsyncError(e, 'unreachable')) {
        lastErr = e;
        continue;
      }
      throw e; // the computer answered: don't try other addresses
    }
  }
  throw lastErr;
}

// ---------- after pairing ----------

export interface SyncResponse {
  computer: { id: string; name: string; os: string };
  messages: Message[];
}

export class PhoneClient {
  readonly key: Uint8Array;
  readonly phoneId: string;
  readonly port: number;
  address: string;

  constructor(c: Computer, address?: string) {
    this.key = fromBase64(c.key);
    this.phoneId = c.phoneId;
    this.port = c.port;
    this.address = address ?? c.address;
  }

  private call<T>(path: string, body: unknown, timeoutMs = 10000): Promise<T> {
    return post<T>(this, `/phone/v1/${path}`, { 'X-Dsync-Phone': this.phoneId }, body, timeoutMs);
  }

  sync(since: number): Promise<SyncResponse> {
    return this.call<SyncResponse>('sync', { since }, 5000);
  }

  async sendText(text: string): Promise<Message> {
    if (utf8Encode(text).length > MAX_TEXT_BYTES) throw new DsyncError('tooBig', 'Text is too long (at most 1 MB)', undefined, 'errors.tooLong');
    const r = await this.call<{ message: Message }>('text', { text }, 20000);
    return r.message;
  }

  unpair(): Promise<unknown> {
    return this.call('unpair', {}, 5000);
  }

  /**
   * Sends a local file (file:// URI) with upload/start, upload/chunk, upload/finish.
   * With a transferId the computer keeps what arrived, and a later call with
   * the same id continues from there (computers before 1.0.12 start over).
   * onProgress gets bytes sent so far.
   */
  async upload(
    file: { uri: string; name: string; transferId?: string },
    onProgress: (sent: number, size: number) => void,
    isCanceled: () => boolean = () => false,
  ): Promise<Message> {
    const f = new File(file.uri);
    if (!f.exists) throw new AppError('errors.fileGone', 'The file is no longer on the phone');
    const size = f.size;
    if (typeof size !== 'number' || size < 0) throw new AppError('errors.cantRead', 'Could not read the file');
    const begin = async () => {
      const r = await this.call<{ uploadId: string; chunkSize: number; offset?: number }>('upload/start', {
        name: file.name,
        size,
        ...(file.transferId ? { transferId: file.transferId } : {}),
      });
      const off = typeof r.offset === 'number' && r.offset > 0 && r.offset <= size ? r.offset : 0;
      return { id: r.uploadId, chunk: Math.max(1, Math.floor(r.chunkSize || 512 * 1024)), offset: off };
    };
    let start = await begin();
    let offset = start.offset;
    onProgress(offset, size);
    if (offset < size) {
      const h = f.open(FileMode.ReadOnly);
      let resyncs = 0;
      try {
        while (offset < size) {
          if (isCanceled()) throw new AppError('errors.canceled', 'Canceled');
          h.offset = offset;
          const bytes = h.readBytes(Math.min(start.chunk, size - offset));
          if (bytes.length === 0) throw new AppError('errors.endedEarly', 'The file ended early');
          let r: { received: number };
          try {
            r = await this.call<{ received: number }>(
              'upload/chunk',
              { uploadId: start.id, offset, data: toBase64(bytes) },
              60000,
            );
          } catch (e) {
            // The computer has a different amount (a chunk got lost, or it
            // restarted): ask it where to continue.
            if (file.transferId && resyncs < 3 && (isDsyncError(e, 'badRequest') || isDsyncError(e, 'notFound'))) {
              resyncs++;
              start = await begin();
              offset = start.offset;
              onProgress(offset, size);
              continue;
            }
            throw e;
          }
          if (r.received !== offset + bytes.length) throw new AppError('errors.lostPart', 'The computer lost part of the file');
          offset = r.received;
          onProgress(offset, size);
        }
      } finally {
        h.close();
      }
    }
    const done = await this.call<{ message: Message }>('upload/finish', { uploadId: start.id }, 30000);
    return done.message;
  }

  /**
   * Downloads a file message from the computer into dest, in chunks of at
   * most 1 MB. If dest already holds the start of the file (an earlier,
   * interrupted download), it continues from there. onProgress gets bytes
   * received so far.
   */
  async download(messageId: number, dest: File, onProgress: (got: number, size: number) => void, expectedSize?: number): Promise<void> {
    let offset = 0;
    if (dest.exists) {
      const have = dest.size ?? 0;
      if (expectedSize !== undefined && have > 0 && have <= expectedSize) offset = have;
      else dest.delete();
    }
    if (!dest.exists) dest.create({ intermediates: true });
    if (expectedSize !== undefined && offset === expectedSize && offset > 0) {
      onProgress(offset, offset);
      return;
    }
    const h = dest.open(FileMode.ReadWrite);
    try {
      h.offset = offset;
      onProgress(offset, expectedSize ?? 0);
      for (;;) {
        const r = await this.call<{ data: string; size: number; eof: boolean }>(
          'download',
          { messageId, offset, length: DOWNLOAD_CHUNK },
          60000,
        );
        if (offset > r.size) throw new AppError('errors.stalled', 'The file on the computer changed');
        const bytes = fromBase64(r.data || '');
        if (bytes.length > 0) h.writeBytes(bytes);
        offset += bytes.length;
        onProgress(offset, r.size);
        if (r.eof || offset >= r.size) break;
        if (bytes.length === 0) throw new AppError('errors.stalled', 'The download stalled');
      }
    } finally {
      // What arrived stays in dest, so the next try continues from it.
      h.close();
    }
  }
}
