import { useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import * as Clipboard from 'expo-clipboard';
import * as DocumentPicker from 'expo-document-picker';
import * as ImagePicker from 'expo-image-picker';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import { type Message, describeError, isPhoneOS } from '../dsync';
import { dayLabel, fileKind, fmtShort, fmtSize, fmtTime, sameDay, type FileKind } from '../format';
import {
  EMPTY_THREAD,
  NO_PENDING,
  dismissPending,
  downloadKey,
  isOnline,
  pollOnce,
  retryPending,
  saveOrShare,
  sendFile,
  sendText,
  useStore,
  type Download,
  type Pending,
} from '../store';
import { type Theme, mono, useTheme } from '../theme';
import { type Direction, type I18n, side, useDirection, useI18n } from '../i18n';
import { Avatar, BackButton, Header, Icon, IconButton, useForeground, useInterval, useNow, type IconName } from '../components/ui';

type Item =
  | { type: 'day'; key: string; label: string }
  | { type: 'msg'; key: string; m: Message; first: boolean }
  | { type: 'pending'; key: string; p: Pending; first: boolean };

const kindIcon: Record<FileKind, IconName> = {
  image: 'image-outline',
  video: 'videocam-outline',
  audio: 'musical-notes-outline',
  archive: 'archive-outline',
  code: 'code-slash-outline',
  doc: 'document-text-outline',
  file: 'document-outline',
};
const kindHue: Record<FileKind, number> = { image: 160, video: 335, audio: 280, archive: 35, code: 200, doc: 215, file: 245 };

// Let the attach sheet finish closing first: iOS won't present a picker over a closing modal.
const sheetClosed = () => new Promise((r) => setTimeout(r, Platform.OS === 'ios' ? 400 : 50));

function nameFromUri(uri: string, fallback: string): string {
  const last = decodeURIComponent(uri.split('?')[0].split('/').pop() || '');
  return last || fallback;
}

export default function ChatScreen({ cid, nav }: { cid: string; nav: Nav }) {
  const t = useTheme();
  const { t: tr } = useI18n();
  const dir = useDirection();
  const { row: dirRow, start } = dir;
  const insets = useSafeAreaInsets();
  const computer = useStore((s) => s.computers.find((c) => c.id === cid));
  const thread = useStore((s) => s.threads[cid] ?? EMPTY_THREAD);
  const pending = useStore((s) => s.pending[cid] ?? NO_PENDING);
  const downloads = useStore((s) => s.downloads);
  const foreground = useForeground();
  const now = useNow(1000);
  const [text, setText] = useState('');
  const [attachOpen, setAttachOpen] = useState(false);
  const [copied, setCopied] = useState<number | null>(null);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useInterval(() => pollOnce(cid), 2000, foreground && !!computer);

  const items = useMemo(() => {
    const out: Item[] = [];
    let prevTime = 0;
    let prevOut: boolean | null = null;
    const push = (time: number, fromPhone: boolean, make: (first: boolean) => Item) => {
      let first = prevOut !== fromPhone;
      if (!prevTime || !sameDay(prevTime, time)) {
        out.push({ type: 'day', key: `d${time}`, label: dayLabel(time) });
        first = true;
      }
      out.push(make(first));
      prevTime = time;
      prevOut = fromPhone;
    };
    // The other side lists a file as soon as its upload starts ("active"). While
    // this phone is still sending it, the sending bubble below stands for it.
    const sending = new Set(pending.filter((p) => p.kind === 'file').map((p) => `${p.name}\u0000${p.size}`));
    const shown = thread.messages.filter(
      (m) => !(m.fromPhone && m.file?.status === 'active' && sending.has(`${m.file.name}\u0000${m.file.size}`)),
    );
    for (const m of shown) push(m.time, m.fromPhone, (first) => ({ type: 'msg', key: `m${m.id}`, m, first }));
    for (const p of pending) push(p.time, true, (first) => ({ type: 'pending', key: p.key, p, first }));
    return out.reverse(); // the list is inverted: newest at the bottom
  }, [thread.messages, pending, tr]); // tr: day labels follow the language

  if (!computer) {
    return (
      <View style={{ flex: 1 }}>
        <Header left={<BackButton onPress={nav.back} />} title={null} />
      </View>
    );
  }

  const online = isOnline(thread, now);
  let status: { text: string; color: string };
  if (online) status = { text: tr('chat.online'), color: t.online };
  else if (thread.error?.kind === 'unauthorized') status = { text: tr('chat.notPaired'), color: t.danger };
  else if (!thread.lastOk && !thread.error) status = { text: tr('chat.connecting'), color: t.muted };
  else status = { text: thread.lastOk ? tr('chat.offlineSeen', { time: fmtShort(thread.lastOk) }) : tr('chat.offline'), color: t.muted };

  const showBanner = !online && thread.error && ['unauthorized', 'clock', 'badReply', 'unreachable'].includes(thread.error.kind);
  let banner = '';
  if (thread.error) {
    switch (thread.error.kind) {
      case 'unauthorized':
        banner = isPhoneOS(computer.os) ? tr('chat.bannerUnauthorizedPhone', { name: computer.name }) : tr('chat.bannerUnauthorized');
        break;
      case 'clock':
        banner = tr('chat.bannerClock');
        break;
      case 'badReply':
        banner = tr('chat.bannerBadReply');
        break;
      default:
        banner = isPhoneOS(computer.os) ? tr('chat.bannerUnreachablePhone', { name: computer.name }) : tr('chat.bannerUnreachable');
    }
  }

  const copy = async (m: Message) => {
    await Clipboard.setStringAsync(m.text ?? '');
    setCopied(m.id);
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
    copiedTimer.current = setTimeout(() => setCopied(null), 1500);
  };

  const send = () => {
    if (!text.trim()) return;
    sendText(cid, text);
    setText('');
  };

  const paste = async () => {
    const clip = await Clipboard.getStringAsync();
    if (clip) setText((cur) => cur + clip);
  };

  const pickMedia = async () => {
    setAttachOpen(false);
    await sheetClosed();
    const r = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ['images', 'videos'],
      allowsMultipleSelection: true,
      quality: 1,
    });
    if (r.canceled) return;
    for (const a of r.assets) {
      sendFile(cid, {
        uri: a.uri,
        name: a.fileName || nameFromUri(a.uri, a.type === 'video' ? 'video.mp4' : 'photo.jpg'),
        size: a.fileSize,
      });
    }
  };

  const pickFile = async () => {
    setAttachOpen(false);
    await sheetClosed();
    const r = await DocumentPicker.getDocumentAsync({ copyToCacheDirectory: true, multiple: true });
    if (r.canceled) return;
    for (const a of r.assets) sendFile(cid, { uri: a.uri, name: a.name || nameFromUri(a.uri, 'file'), size: a.size });
  };

  const render = ({ item }: { item: Item }) => {
    if (item.type === 'day') {
      return (
        <View style={s.day}>
          <Text style={[s.dayText, { color: t.muted, backgroundColor: t.raised, borderColor: t.border }]}>{item.label}</Text>
        </View>
      );
    }
    if (item.type === 'pending') return <PendingBubble t={t} tr={tr} dir={dir} cid={cid} p={item.p} first={item.first} />;
    const m = item.m;
    if (m.file) {
      return (
        <FileBubble
          t={t}
          tr={tr}
          dir={dir}
          m={m}
          first={item.first}
          download={downloads[downloadKey(cid, m.id)]}
          onSave={() => void saveOrShare(cid, m)}
        />
      );
    }
    const out = m.fromPhone;
    return (
      <View style={[s.msg, msgSide(dir, out), item.first && s.first]}>
        <Pressable
          onLongPress={() => void copy(m)}
          style={[s.bubble, bubbleColors(t, out), item.first && firstCorner(dir, out)]}
        >
          <Text selectable style={[s.mono, { color: out ? t.bubbleOutText : t.text }]}>
            {m.text}
          </Text>
          <Text style={[s.stamp, { color: out ? t.bubbleOutMuted : t.muted, textAlign: dir.end }]}>{fmtTime(m.time)}</Text>
        </Pressable>
        <IconButton
          icon={copied === m.id ? 'checkmark' : 'copy-outline'}
          label={tr('chat.copy')}
          size={17}
          color={copied === m.id ? t.ok : t.muted}
          onPress={() => void copy(m)}
          style={{ width: 32, height: 32 }}
        />
      </View>
    );
  };

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
      <Header
        left={<BackButton onPress={nav.back} />}
        title={
          <Pressable onPress={() => nav.go({ name: 'settings', cid })} style={[s.headTitle, { flexDirection: dirRow }]}>
            <Avatar name={computer.name} id={computer.id} size={36} online={online} ringColor={t.panel} />
            <View style={{ flex: 1, minWidth: 0 }}>
              <Text numberOfLines={1} style={[s.headName, { color: t.text, textAlign: start }]}>
                {computer.name}
              </Text>
              <Text numberOfLines={1} style={[s.headStatus, { color: status.color, textAlign: start }]}>
                {status.text}
              </Text>
            </View>
          </Pressable>
        }
        right={<IconButton icon="settings-outline" label={isPhoneOS(computer.os) ? tr('chat.phoneSettings') : tr('chat.computerSettings')} onPress={() => nav.go({ name: 'settings', cid })} />}
      />
      {showBanner ? (
        <View style={[s.banner, { flexDirection: dirRow, backgroundColor: t.dangerSoft }]}>
          <Icon name="alert-circle" size={17} color={t.danger} />
          <Text style={[s.bannerText, { color: t.danger, textAlign: start }]}>{banner}</Text>
        </View>
      ) : null}

      {items.length === 0 ? (
        <View style={s.empty}>
          {thread.loaded ? (
            <>
              <View style={[s.emptyIcon, { backgroundColor: t.accentSoft }]}>
                <Icon name="chatbubbles-outline" size={28} color={t.accent} />
              </View>
              <Text style={[s.emptyTitle, { color: t.text }]}>{tr('chat.emptyTitle')}</Text>
              <Text style={[s.emptyText, { color: t.muted }]}>{tr('chat.emptyText', { name: computer.name })}</Text>
            </>
          ) : (
            <ActivityIndicator color={t.accent} />
          )}
        </View>
      ) : (
        <FlatList
          inverted
          data={items}
          keyExtractor={(i) => i.key}
          renderItem={render}
          contentContainerStyle={{ paddingHorizontal: 12, paddingVertical: 10 }}
          keyboardShouldPersistTaps="handled"
        />
      )}

      <View style={[s.composerWrap, { paddingBottom: Math.max(insets.bottom, 10) }]}>
        <View style={[s.composer, { flexDirection: dirRow, backgroundColor: t.raised, borderColor: t.borderStrong }]}>
          <IconButton icon="add" label={tr('chat.attach')} size={24} onPress={() => setAttachOpen(true)} />
          <TextInput
            value={text}
            onChangeText={setText}
            // An isolate keeps the Arabic placeholder in order even where the field's own direction is LTR (web).
            placeholder={dir.isRTL ? `\u2067${tr('chat.placeholder', { name: computer.name })}\u2069` : tr('chat.placeholder', { name: computer.name })}
            placeholderTextColor={t.muted}
            multiline
            // The placeholder starts at the reading side; typed text keeps its own direction.
            style={[s.input, { color: t.text }, !text && { textAlign: start }]}
          />
          <IconButton icon="clipboard-outline" label={tr('chat.pasteClipboard')} size={20} onPress={() => void paste()} />
          <Pressable
            onPress={send}
            disabled={!text.trim()}
            accessibilityLabel={tr('chat.send')}
            style={({ pressed }) => [s.send, { backgroundColor: t.accent, opacity: !text.trim() ? 0.45 : pressed ? 0.8 : 1 }]}
          >
            <Icon name="arrow-up" size={20} color={t.accentText} />
          </Pressable>
        </View>
      </View>

      <Modal visible={attachOpen} transparent animationType="fade" onRequestClose={() => setAttachOpen(false)}>
        <Pressable style={s.backdrop} onPress={() => setAttachOpen(false)}>
          <View style={[s.sheet, { backgroundColor: t.panel, paddingBottom: insets.bottom + 12 }]}>
            <SheetRow t={t} dir={dir} icon="images-outline" title={tr('chat.photoOrVideo')} onPress={() => void pickMedia()} />
            <SheetRow t={t} dir={dir} icon="document-outline" title={tr('chat.file')} onPress={() => void pickFile()} />
            <SheetRow t={t} dir={dir} icon="close" title={tr('common.cancel')} muted onPress={() => setAttachOpen(false)} />
          </View>
        </Pressable>
      </Modal>
    </KeyboardAvoidingView>
  );
}

function bubbleColors(t: Theme, out: boolean) {
  return out
    ? { backgroundColor: t.bubbleOut, borderColor: 'transparent' }
    : { backgroundColor: t.bubbleIn, borderColor: t.border };
}

/** Which side a bubble sits on: the phone's own on the end side (right, or left in Arabic). */
function msgSide(dir: Direction, out: boolean) {
  return { flexDirection: out !== dir.isRTL ? 'row-reverse' : 'row' } as const;
}

/** The first bubble of a run gets a sharp corner towards its side. */
function firstCorner(dir: Direction, out: boolean) {
  return side(dir.isRTL, out ? { borderTopRightRadius: 5 } : { borderTopLeftRadius: 5 });
}

function SheetRow({ t, dir, icon, title, onPress, muted }: { t: Theme; dir: Direction; icon: IconName; title: string; onPress: () => void; muted?: boolean }) {
  return (
    <Pressable onPress={onPress} style={({ pressed }) => [s.sheetRow, { flexDirection: dir.row, backgroundColor: pressed ? t.hover : 'transparent' }]}>
      <View style={[s.sheetIcon, { backgroundColor: muted ? t.hover : t.accentSoft }]}>
        <Icon name={icon} size={20} color={muted ? t.muted : t.accent} />
      </View>
      <Text style={[s.sheetText, { color: muted ? t.muted : t.text, textAlign: dir.start }]}>{title}</Text>
    </Pressable>
  );
}

function FileIcon({ t, name, out }: { t: Theme; name: string; out: boolean }) {
  const k = fileKind(name);
  const bg = out ? 'rgba(255,255,255,0.18)' : `hsl(${kindHue[k]}, ${t.avatarS}%, ${t.avatarLBg}%)`;
  const fg = out ? '#fff' : `hsl(${kindHue[k]}, ${t.avatarS}%, ${t.avatarLFg}%)`;
  return (
    <View style={[s.fileIcon, { backgroundColor: bg }]}>
      <Icon name={kindIcon[k]} size={22} color={fg} />
    </View>
  );
}

function Bar({ t, dir, out, frac }: { t: Theme; dir: Direction; out: boolean; frac: number }) {
  return (
    <View style={[s.bar, { backgroundColor: out ? 'rgba(255,255,255,0.22)' : t.hover }]}>
      <View style={[s.fill, { alignSelf: dir.isRTL ? 'flex-end' : 'flex-start', width: `${Math.round(Math.min(1, Math.max(0, frac)) * 100)}%`, backgroundColor: out ? '#fff' : t.accent }]} />
    </View>
  );
}

function Pill({ t, dir, out, icon, title, onPress, disabled }: { t: Theme; dir: Direction; out: boolean; icon: IconName; title: string; onPress: () => void; disabled?: boolean }) {
  const fg = out ? '#fff' : t.accent;
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      style={({ pressed }) => [
        s.pill,
        { flexDirection: dir.row, backgroundColor: out ? 'rgba(255,255,255,0.16)' : t.accentSoft, opacity: disabled ? 0.5 : pressed ? 0.75 : 1 },
      ]}
    >
      <Icon name={icon} size={15} color={fg} />
      <Text style={[s.pillText, { color: fg }]}>{title}</Text>
    </Pressable>
  );
}

type BubbleProps = { t: Theme; tr: I18n['t']; dir: Direction; first: boolean };

function FileBubble({ t, tr, dir, m, first, download, onSave }: BubbleProps & { m: Message; download?: Download; onSave: () => void }) {
  const f = m.file!;
  const out = m.fromPhone;
  const sub = out ? t.bubbleOutMuted : t.muted;
  let state = '';
  if (f.status === 'active') state = out ? tr('chat.sending') : tr('chat.receiving');
  else if (f.status === 'failed') state = tr('chat.failed');
  else if (f.status === 'canceled') state = tr('chat.canceled');
  else if (out) state = tr('chat.sent');
  const canSave = !out && f.status === 'done';
  const busy = !!download?.busy;
  // Arriving from a phone that scanned this phone's code.
  const arriving = !out && f.status === 'active' && typeof f.received === 'number';
  return (
    <View style={[s.msg, msgSide(dir, out), first && s.first]}>
      <View style={[s.bubble, s.fileBubble, bubbleColors(t, out), first && firstCorner(dir, out)]}>
        <View style={[s.fileCard, { flexDirection: dir.row }]}>
          <FileIcon t={t} name={f.name} out={out} />
          <View style={{ flex: 1, minWidth: 0 }}>
            <FileName name={f.name} dir={dir} color={out ? t.bubbleOutText : t.text} />
            <Text style={[s.fileSubText, { color: sub, textAlign: dir.start }]}>
              {busy
                ? tr('chat.progress', { done: fmtSize(download!.got), total: fmtSize(f.size) })
                : arriving
                  ? tr('chat.progress', { done: fmtSize(f.received!), total: fmtSize(f.size) })
                  : fmtSize(f.size)}
              {state ? ` · ${state}` : ''}
            </Text>
          </View>
        </View>
        {busy ? <Bar t={t} dir={dir} out={out} frac={f.size ? download!.got / f.size : 0} /> : null}
        {arriving ? <Bar t={t} dir={dir} out={out} frac={f.size ? f.received! / f.size : 0} /> : null}
        {/* f.error comes from the computer, in its own words. */}
        {f.error ? <Text numberOfLines={2} style={[s.fileError, { color: out ? '#ffd0d0' : t.danger, textAlign: dir.start }]}>{f.error}</Text> : null}
        {download?.error != null && !busy ? (
          <Text numberOfLines={3} style={[s.fileError, { color: t.danger, textAlign: dir.start }]}>{describeError(download.error)}</Text>
        ) : null}
        {canSave ? (
          <View style={[s.fileActions, { flexDirection: dir.row }]}>
            <Pill
              t={t}
              dir={dir}
              out={out}
              icon={busy ? 'hourglass-outline' : download?.error != null ? 'refresh' : 'share-outline'}
              title={busy ? tr('chat.downloading') : download?.error != null ? tr('chat.retry') : tr('chat.saveShare')}
              disabled={busy}
              onPress={onSave}
            />
          </View>
        ) : null}
        <Text style={[s.stamp, { color: sub, textAlign: dir.end }]}>{fmtTime(m.time)}</Text>
      </View>
    </View>
  );
}

/** A file name, aligned to the reading side; its own letters decide its direction. */
function FileName({ name, dir, color }: { name: string; dir: Direction; color: string }) {
  return (
    <Text numberOfLines={1} style={[s.fileName, { color, textAlign: dir.start }]}>
      {name}
    </Text>
  );
}

function PendingBubble({ t, tr, dir, cid, p, first }: BubbleProps & { cid: string; p: Pending }) {
  const failed = p.status === 'failed';
  const actions = failed ? (
    <View style={[s.fileActions, { flexDirection: dir.row }]}>
      <Pill t={t} dir={dir} out icon="refresh" title={tr('chat.retry')} onPress={() => retryPending(cid, p.key)} />
      <Pill t={t} dir={dir} out icon="close" title={tr('chat.discard')} onPress={() => dismissPending(cid, p.key)} />
    </View>
  ) : null;
  const err =
    failed && p.error != null ? <Text style={[s.fileError, { color: '#ffd0d0', textAlign: dir.start }]}>{describeError(p.error)}</Text> : null;
  if (p.kind === 'text') {
    return (
      <View style={[s.msg, msgSide(dir, true), first && s.first]}>
        <View style={[s.bubble, bubbleColors(t, true), first && firstCorner(dir, true), { opacity: failed ? 1 : 0.75 }]}>
          <Text style={[s.mono, { color: t.bubbleOutText }]}>{p.text}</Text>
          {err}
          {actions}
          <Text style={[s.stamp, { color: t.bubbleOutMuted, textAlign: dir.end }]}>{failed ? tr('chat.notSent') : tr('chat.sending')}</Text>
        </View>
      </View>
    );
  }
  return (
    <View style={[s.msg, msgSide(dir, true), first && s.first]}>
      <View style={[s.bubble, s.fileBubble, bubbleColors(t, true), first && firstCorner(dir, true)]}>
        <View style={[s.fileCard, { flexDirection: dir.row }]}>
          <FileIcon t={t} name={p.name ?? ''} out />
          <View style={{ flex: 1, minWidth: 0 }}>
            <FileName name={p.name ?? ''} dir={dir} color={t.bubbleOutText} />
            <Text style={[s.fileSubText, { color: t.bubbleOutMuted, textAlign: dir.start }]}>
              {failed ? `${fmtSize(p.size)} · ${tr('chat.failed')}` : tr('chat.progress', { done: fmtSize(p.sent), total: fmtSize(p.size) })}
            </Text>
          </View>
        </View>
        {!failed ? <Bar t={t} dir={dir} out frac={p.size ? p.sent / p.size : 0} /> : null}
        {err}
        {actions}
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  headTitle: { alignItems: 'center', gap: 10 },
  headName: { fontSize: 15.5, fontWeight: '600' },
  headStatus: { fontSize: 12, fontWeight: '500', marginTop: 1 },
  banner: { gap: 8, alignItems: 'flex-start', paddingHorizontal: 14, paddingVertical: 9 },
  bannerText: { flex: 1, fontSize: 13, fontWeight: '500', lineHeight: 18 },
  day: { alignItems: 'center', marginTop: 16, marginBottom: 6 },
  dayText: {
    fontSize: 11.5,
    fontWeight: '600',
    paddingHorizontal: 12,
    paddingVertical: 3,
    borderRadius: 999,
    borderWidth: 1,
    overflow: 'hidden',
  },
  msg: { alignItems: 'center', gap: 4, marginTop: 3 },
  first: { marginTop: 12 },
  bubble: {
    maxWidth: '80%',
    paddingHorizontal: 12,
    paddingTop: 8,
    paddingBottom: 6,
    borderRadius: 14,
    borderWidth: 1,
  },
  mono: { fontFamily: mono, fontSize: 13.5, lineHeight: 20 },
  stamp: { fontSize: 10.5, marginTop: 3 },
  fileBubble: { width: 300, maxWidth: '85%', paddingTop: 10 },
  fileCard: { alignItems: 'center', gap: 11 },
  fileIcon: { width: 42, height: 42, borderRadius: 10, alignItems: 'center', justifyContent: 'center' },
  fileName: { fontSize: 14.5, fontWeight: '600' },
  fileSubText: { fontSize: 12, marginTop: 2 },
  fileError: { fontSize: 12, marginTop: 4 },
  bar: { height: 6, borderRadius: 3, overflow: 'hidden', marginTop: 8, marginBottom: 2 },
  fill: { height: '100%', borderRadius: 3 },
  fileActions: { flexWrap: 'wrap', gap: 6, marginTop: 8 },
  pill: { alignItems: 'center', gap: 5, paddingHorizontal: 11, paddingVertical: 6, borderRadius: 999 },
  pillText: { fontSize: 13, fontWeight: '600' },
  empty: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 32 },
  emptyIcon: { width: 60, height: 60, borderRadius: 30, alignItems: 'center', justifyContent: 'center' },
  emptyTitle: { marginTop: 12, fontSize: 16, fontWeight: '600' },
  emptyText: { marginTop: 4, fontSize: 14, textAlign: 'center', lineHeight: 20, maxWidth: 300 },
  composerWrap: { paddingHorizontal: 10, paddingTop: 6 },
  composer: { alignItems: 'flex-end', gap: 2, padding: 5, borderRadius: 16, borderWidth: 1 },
  input: { flex: 1, minHeight: 38, maxHeight: 140, paddingHorizontal: 4, paddingTop: 9, paddingBottom: 9, fontFamily: mono, fontSize: 14 },
  send: { width: 38, height: 38, borderRadius: 11, alignItems: 'center', justifyContent: 'center' },
  backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.35)', justifyContent: 'flex-end' },
  sheet: { borderTopLeftRadius: 18, borderTopRightRadius: 18, paddingTop: 10, paddingHorizontal: 10 },
  sheetRow: { alignItems: 'center', gap: 14, padding: 12, borderRadius: 12 },
  sheetIcon: { width: 38, height: 38, borderRadius: 10, alignItems: 'center', justifyContent: 'center' },
  sheetText: { flex: 1, fontSize: 15.5, fontWeight: '500' },
});
