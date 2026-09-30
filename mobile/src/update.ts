// Updates for the phone app, from dsync's GitHub releases (the same place the
// desktop app updates from). Android installs the new APK over this one; the
// system only accepts it when it's signed with the same key. iOS can't
// install apps itself: it opens the release page for AltStore / Sideloadly.

import { useSyncExternalStore } from 'react';
import { Linking, Platform } from 'react-native';
import * as Application from 'expo-application';
import * as SecureStore from 'expo-secure-store';

const REPO = 'muhammedaslan34/dsync';
const LATEST = `https://api.github.com/repos/${REPO}/releases/latest`;
const CHECK_EVERY = 24 * 60 * 60 * 1000;
const LAST_CHECK_KEY = 'dsync.updateChecked';

export interface Release {
  version: string;
  page: string;
  apk?: string; // download URL of the Android APK
}

export type UpdateState =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'current' }
  | { kind: 'available'; release: Release }
  | { kind: 'error'; what: 'check' | 'install'; release?: Release };

let state: UpdateState = { kind: 'idle' };
const listeners = new Set<() => void>();

function set(s: UpdateState) {
  state = s;
  listeners.forEach((l) => l());
}

export function useUpdate(): UpdateState {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => state,
  );
}

/** This app's version, like "1.0.11". */
export function appVersion(): string {
  return Application.nativeApplicationVersion ?? '0';
}

/** Compares "1.0.12" and "1.0.9" part by part; >0 when a is newer. */
export function compareVersions(a: string, b: string): number {
  const pa = a.replace(/^v/, '').split(/[.-]/).map((x) => parseInt(x, 10) || 0);
  const pb = b.replace(/^v/, '').split(/[.-]/).map((x) => parseInt(x, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d !== 0) return d;
  }
  return 0;
}

/** Asks GitHub for the newest release. */
export async function checkForUpdate(): Promise<void> {
  if (state.kind === 'checking') return;
  set({ kind: 'checking' });
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 15000);
    let res: Response;
    try {
      res = await fetch(LATEST, { headers: { Accept: 'application/vnd.github+json' }, signal: ctrl.signal });
    } finally {
      clearTimeout(timer);
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const r = (await res.json()) as { tag_name?: string; html_url?: string; assets?: { name: string; browser_download_url: string }[] };
    const version = (r.tag_name ?? '').replace(/^v/, '');
    if (!version) throw new Error('no version');
    const apk = r.assets?.find((a) => /-android\.apk$/.test(a.name))?.browser_download_url;
    const release: Release = { version, page: r.html_url ?? `https://github.com/${REPO}/releases/latest`, apk };
    try {
      await SecureStore.setItemAsync(LAST_CHECK_KEY, String(Date.now()));
    } catch {}
    set(compareVersions(version, appVersion()) > 0 ? { kind: 'available', release } : { kind: 'current' });
  } catch {
    set({ kind: 'error', what: 'check' });
  }
}

/** At startup: checks at most once a day, quietly. */
export async function checkForUpdateDaily() {
  try {
    const last = Number((await SecureStore.getItemAsync(LAST_CHECK_KEY)) ?? 0);
    if (Date.now() - last < CHECK_EVERY) return;
  } catch {}
  await checkForUpdate();
  if (state.kind === 'error') set({ kind: 'idle' }); // no network: say nothing
}

/**
 * Android: opens the new APK's download in the browser; tapping the finished
 * download installs it over this app (the same way it was installed). This
 * keeps the app from needing the "install other apps" permission, which
 * Play Protect treats as a warning sign. iOS: opens the release page.
 */
export async function installUpdate(release: Release): Promise<void> {
  try {
    await Linking.openURL(Platform.OS === 'android' && release.apk ? release.apk : release.page);
  } catch {
    set({ kind: 'error', what: 'install', release });
  }
}
