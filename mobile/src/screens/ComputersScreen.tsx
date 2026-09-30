import { FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import type { Computer, Message } from '../dsync';
import { fmtShort, osLabel } from '../format';
import { EMPTY_THREAD, isOnline, pollOnce, useStore, type Thread } from '../store';
import { useTheme } from '../theme';
import { Avatar, Button, Header, Icon, Logo, useForeground, useInterval, useNow } from '../components/ui';

function preview(m: Message | undefined): string {
  if (!m) return '';
  const who = m.fromPhone ? 'You: ' : '';
  if (m.file) return `${who}${m.file.name}`;
  return who + (m.text ?? '').replace(/\s+/g, ' ').trim();
}

export default function ComputersScreen({ nav }: { nav: Nav }) {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const computers = useStore((s) => s.computers);
  const threads = useStore((s) => s.threads);
  const foreground = useForeground();
  const now = useNow(1000);

  // Keep online dots and previews fresh while this list is visible.
  useInterval(() => Promise.all(computers.map((c) => pollOnce(c.id))), 3000, foreground && computers.length > 0);

  const row = ({ item: c }: { item: Computer }) => {
    const th: Thread = threads[c.id] ?? EMPTY_THREAD;
    const last = th.messages[th.messages.length - 1];
    const online = isOnline(th, now);
    let sub = preview(last);
    if (!sub) sub = th.loaded ? 'No messages yet' : osLabel[c.os] ?? '';
    return (
      <Pressable
        onPress={() => nav.go({ name: 'chat', cid: c.id })}
        style={({ pressed }) => [
          s.row,
          { backgroundColor: t.raised, borderColor: t.border, opacity: pressed ? 0.85 : 1 },
        ]}
      >
        <Avatar name={c.name} id={c.id} size={44} online={online} ringColor={t.raised} />
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <View style={s.rowTop}>
            <Text numberOfLines={1} style={[s.name, { color: t.text }]}>
              {c.name}
            </Text>
            {last ? <Text style={[s.time, { color: t.muted }]}>{fmtShort(last.time)}</Text> : null}
          </View>
          <Text numberOfLines={1} style={[s.preview, { color: t.muted }]}>
            {sub}
          </Text>
        </View>
      </Pressable>
    );
  };

  return (
    <View style={{ flex: 1 }}>
      <Header
        left={<View style={{ paddingLeft: 6 }}><Logo /></View>}
        title={<Text style={[s.brand, { color: t.text }]}>dsync</Text>}
      />
      {computers.length === 0 ? (
        <View style={s.empty}>
          <View style={[s.pulse, { backgroundColor: t.accentSoft }]}>
            <Icon name="laptop-outline" size={30} color={t.accent} />
          </View>
          <Text style={[s.emptyTitle, { color: t.text }]}>No computers yet</Text>
          <Text style={[s.emptyText, { color: t.muted }]}>
            On your computer, open dsync → Settings → Connect a phone, then scan the code it shows.
          </Text>
        </View>
      ) : (
        <FlatList
          data={computers}
          keyExtractor={(c) => c.id}
          renderItem={row}
          contentContainerStyle={{ padding: 12, gap: 8 }}
          ListHeaderComponent={<Text style={[s.label, { color: t.muted }]}>COMPUTERS</Text>}
        />
      )}
      <View style={{ padding: 16, paddingBottom: insets.bottom + 16 }}>
        <Button title="Connect a computer" icon="qr-code-outline" onPress={() => nav.go({ name: 'scan' })} />
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  brand: { fontSize: 19, fontWeight: '700', letterSpacing: -0.2 },
  label: { fontSize: 11, fontWeight: '600', letterSpacing: 0.7, marginLeft: 6, marginBottom: 2 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    padding: 12,
    borderRadius: 12,
    borderWidth: 1,
  },
  rowTop: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  name: { flex: 1, fontSize: 15.5, fontWeight: '600' },
  time: { fontSize: 11.5 },
  preview: { fontSize: 13.5 },
  empty: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: 32 },
  pulse: { width: 64, height: 64, borderRadius: 32, alignItems: 'center', justifyContent: 'center' },
  emptyTitle: { marginTop: 14, fontSize: 17, fontWeight: '600' },
  emptyText: { marginTop: 6, fontSize: 14, textAlign: 'center', lineHeight: 20, maxWidth: 300 },
});
