// Keeps transfers going when the user leaves the app. On Android an ongoing
// notification ("Sending report.mp4", 45 %) runs a foreground service, so the
// app keeps running until the transfers are done. iOS has no such service:
// transfers pause there and continue when the app is back (see store.ts).
import { PermissionsAndroid, Platform } from 'react-native';
import native from '../modules/dsync-background';
import { t } from './i18n';
import { getState, subscribe, type State } from './store';

interface Summary {
  title: string;
  text: string;
  progress: number;
  upload: boolean;
}

function summarize(s: State): Summary | null {
  let files = 0;
  let done = 0;
  let total = 0;
  let first: { name: string; upload: boolean } | null = null;
  for (const list of Object.values(s.pending)) {
    for (const p of list) {
      if (p.status !== 'sending') continue;
      files++;
      done += p.kind === 'file' ? p.sent : 0;
      total += p.kind === 'file' ? p.size : 0;
      first ??= { name: p.kind === 'file' ? p.name ?? '' : t('bg.message'), upload: true };
    }
  }
  for (const [k, d] of Object.entries(s.downloads)) {
    if (!d.busy) continue;
    files++;
    done += d.got;
    total += d.size;
    first ??= { name: d.name ?? k, upload: false };
  }
  // Files another phone is sending to this one (phone to phone, see host.ts).
  for (const th of Object.values(s.threads)) {
    for (const m of th.messages) {
      if (m.fromPhone || m.file?.status !== 'active' || typeof m.file.received !== 'number') continue;
      files++;
      done += m.file.received;
      total += m.file.size;
      first ??= { name: m.file.name, upload: false };
    }
  }
  if (!first) return null;
  const progress = total > 0 ? Math.floor((done * 100) / total) : -1;
  const parts: string[] = [];
  if (progress >= 0) parts.push(t('bg.percent', { percent: progress }));
  if (files > 1) parts.push(t('bg.more', { count: files - 1 }));
  return {
    title: t(first.upload ? 'bg.sending' : 'bg.receiving', { name: first.name }),
    text: parts.join(' · '),
    progress,
    upload: first.upload,
  };
}

let shown = '';
let asked = false;

function refresh() {
  if (!native) return;
  const sum = summarize(getState());
  const id = sum ? JSON.stringify(sum) : '';
  if (id === shown) return;
  if (!sum) {
    shown = '';
    native.stop();
    return;
  }
  if (!asked && Platform.OS === 'android' && Number(Platform.Version) >= 33) {
    // Android 13+: the notification needs permission (the service runs either way).
    asked = true;
    void PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS).catch(() => {});
  }
  if (native.update(sum.title, sum.text, sum.progress, sum.upload)) shown = id;
}

/** Starts following transfers; call once at startup. */
export function startBackgroundTransfers() {
  if (!native) return;
  subscribe(refresh);
  refresh();
}
