import { useState } from 'react';
import { Alert, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import { describeError } from '../dsync';
import { dayLabel, fmtTime, osLabel } from '../format';
import { EMPTY_THREAD, forgetComputer, isOnline, pollOnce, useStore } from '../store';
import { type Theme, mono, useTheme } from '../theme';
import { Avatar, Button, Card, Header, IconButton, useForeground, useInterval, useNow } from '../components/ui';

function Row({ t, label, value, monoValue, last }: { t: Theme; label: string; value: string; monoValue?: boolean; last?: boolean }) {
  return (
    <View style={[s.row, !last && { borderBottomColor: t.border, borderBottomWidth: StyleSheet.hairlineWidth }]}>
      <Text style={[s.rowLabel, { color: t.muted }]}>{label}</Text>
      <Text selectable style={[s.rowValue, { color: t.text }, monoValue && { fontFamily: mono, fontSize: 13.5 }]}>
        {value}
      </Text>
    </View>
  );
}

export default function SettingsScreen({ cid, nav }: { cid: string; nav: Nav }) {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const computer = useStore((s) => s.computers.find((c) => c.id === cid));
  const thread = useStore((s) => s.threads[cid] ?? EMPTY_THREAD);
  const now = useNow(1000);
  const [busy, setBusy] = useState(false);
  const foreground = useForeground();
  useInterval(() => pollOnce(cid), 2000, foreground && !!computer && !busy);

  if (!computer) return null;
  const online = isOnline(thread, now);
  const others = computer.addresses.filter((a) => a !== computer.address);

  const done = () => nav.go({ name: 'list' });

  const forget = async () => {
    setBusy(true);
    try {
      await forgetComputer(cid);
      done();
    } catch (e) {
      setBusy(false);
      Alert.alert(
        "Couldn't reach the computer",
        `${describeError(e)}\n\nForget it on this phone anyway? The computer will keep listing this phone until you remove it there.`,
        [
          { text: 'Cancel', style: 'cancel' },
          {
            text: 'Forget anyway',
            style: 'destructive',
            onPress: async () => {
              await forgetComputer(cid, true);
              done();
            },
          },
        ],
      );
    }
  };

  const confirmForget = () =>
    Alert.alert(`Forget ${computer.name}?`, 'This phone and the computer will stop syncing. You can connect again with a new code.', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Forget', style: 'destructive', onPress: () => void forget() },
    ]);

  return (
    <View style={{ flex: 1 }}>
      <Header
        left={<IconButton icon="chevron-back" label="Back" onPress={nav.back} color={t.text} />}
        title={<Text style={[s.headTitle, { color: t.text }]}>Computer</Text>}
      />
      <ScrollView contentContainerStyle={{ padding: 16, gap: 16, paddingBottom: insets.bottom + 24 }}>
        <View style={s.top}>
          <Avatar name={computer.name} id={computer.id} size={72} online={online} ringColor={t.bg} />
          <Text style={[s.name, { color: t.text }]}>{computer.name}</Text>
          <Text style={[s.sub, { color: online ? t.online : t.muted }]}>
            {osLabel[computer.os] ?? computer.os}
            {osLabel[computer.os] || computer.os ? ' · ' : ''}
            {online ? 'Online' : 'Offline'}
          </Text>
        </View>

        <Card t={t} style={{ paddingVertical: 4 }}>
          <Row t={t} label="Name" value={computer.name} />
          <Row t={t} label="Address in use" value={`${computer.address}:${computer.port}`} monoValue />
          {others.length > 0 ? <Row t={t} label="Other addresses" value={others.join('\n')} monoValue /> : null}
          <Row t={t} label="Paired" value={`${dayLabel(computer.pairedAt)}, ${fmtTime(computer.pairedAt)}`} last />
        </Card>

        <Button title="Forget this computer" kind="danger" icon="trash-outline" busy={busy} onPress={confirmForget} />
        <Text style={[s.note, { color: t.muted }]}>
          Messages are kept on the computer. To use this phone with it again, show a new code on the computer (Settings → Connect a phone).
        </Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  headTitle: { fontSize: 16, fontWeight: '600' },
  top: { alignItems: 'center', gap: 4, paddingVertical: 8 },
  name: { marginTop: 8, fontSize: 20, fontWeight: '700' },
  sub: { fontSize: 13.5, fontWeight: '500' },
  row: { paddingVertical: 11, gap: 2 },
  rowLabel: { fontSize: 12, fontWeight: '600' },
  rowValue: { fontSize: 15 },
  note: { fontSize: 13, lineHeight: 18, textAlign: 'center', paddingHorizontal: 12 },
});
