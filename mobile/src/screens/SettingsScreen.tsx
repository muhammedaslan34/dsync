import { useState } from 'react';
import { Alert, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import { describeError } from '../dsync';
import { dayLabel, fmtTime, osLabel } from '../format';
import { EMPTY_THREAD, forgetComputer, isOnline, pollOnce, useStore } from '../store';
import { type Theme, mono, useTheme } from '../theme';
import { type Direction, useDirection, useI18n } from '../i18n';
import { Avatar, BackButton, Button, Card, Header, useForeground, useInterval, useNow } from '../components/ui';

function Row({ t, dir, label, value, monoValue, last }: { t: Theme; dir: Direction; label: string; value: string; monoValue?: boolean; last?: boolean }) {
  return (
    <View style={[s.row, !last && { borderBottomColor: t.border, borderBottomWidth: StyleSheet.hairlineWidth }]}>
      <Text style={[s.rowLabel, { color: t.muted, textAlign: dir.start }]}>{label}</Text>
      <Text selectable style={[s.rowValue, { color: t.text, textAlign: dir.start }, monoValue && { fontFamily: mono, fontSize: 13.5 }]}>
        {value}
      </Text>
    </View>
  );
}

export default function SettingsScreen({ cid, nav }: { cid: string; nav: Nav }) {
  const t = useTheme();
  const { t: tr } = useI18n();
  const dir = useDirection();
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
        tr('computer.unreachableTitle'),
        tr('computer.unreachableText', { error: describeError(e) }),
        [
          { text: tr('common.cancel'), style: 'cancel' },
          {
            text: tr('computer.forgetAnyway'),
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
    Alert.alert(tr('computer.confirmTitle', { name: computer.name }), tr('computer.confirmText'), [
      { text: tr('common.cancel'), style: 'cancel' },
      { text: tr('computer.confirm'), style: 'destructive', onPress: () => void forget() },
    ]);

  return (
    <View style={{ flex: 1 }}>
      <Header
        left={<BackButton onPress={nav.back} />}
        title={<Text style={[s.headTitle, { color: t.text, textAlign: dir.start }]}>{tr('computer.title')}</Text>}
      />
      <ScrollView contentContainerStyle={{ padding: 16, gap: 16, paddingBottom: insets.bottom + 24 }}>
        <View style={s.top}>
          <Avatar name={computer.name} id={computer.id} size={72} online={online} ringColor={t.bg} />
          <Text style={[s.name, { color: t.text }]}>{computer.name}</Text>
          <Text style={[s.sub, { color: online ? t.online : t.muted }]}>
            {/* In Arabic the line reads from the right, starting with the system name. */}
            {(dir.isRTL ? '\u200f' : '') +
              [osLabel[computer.os] ?? computer.os, online ? tr('chat.online') : tr('chat.offline')].filter(Boolean).join(' · ')}
          </Text>
        </View>

        <Card t={t} style={{ paddingVertical: 4 }}>
          <Row t={t} dir={dir} label={tr('computer.name')} value={computer.name} />
          <Row t={t} dir={dir} label={tr('computer.address')} value={`${computer.address}:${computer.port}`} monoValue />
          {others.length > 0 ? (
            <Row t={t} dir={dir} label={tr('computer.otherAddresses', { count: others.length })} value={others.join('\n')} monoValue />
          ) : null}
          <Row
            t={t}
            dir={dir}
            label={tr('computer.paired')}
            value={tr('computer.pairedAt', { day: dayLabel(computer.pairedAt), time: fmtTime(computer.pairedAt) })}
            last
          />
        </Card>

        <Button title={tr('computer.forget')} kind="danger" icon="trash-outline" busy={busy} onPress={confirmForget} />
        <Text style={[s.note, { color: t.muted }]}>{tr('computer.note')}</Text>
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
