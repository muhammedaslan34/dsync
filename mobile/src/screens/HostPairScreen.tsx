// "Connect a phone": shows a code for another phone to scan (see host.ts).
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, View, useWindowDimensions } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import { type HostPairing, deviceName, onPhonePaired, startHostPairing, stopHostPairing } from '../host';
import { useDirection, useI18n } from '../i18n';
import { useTheme } from '../theme';
import { BackButton, Button, Card, Header, Icon, useNow } from '../components/ui';
import QRCode from '../components/QRCode';

export default function HostPairScreen({ nav }: { nav: Nav }) {
  const t = useTheme();
  const { t: tr } = useI18n();
  const dir = useDirection();
  const insets = useSafeAreaInsets();
  const { width } = useWindowDimensions();
  const now = useNow(1000);
  const [code, setCode] = useState<HostPairing | null | 'loading' | 'error'>('loading');

  const make = useCallback(() => {
    setCode('loading');
    startHostPairing().then(
      (p) => setCode(p),
      () => setCode('error'),
    );
  }, []);

  useEffect(() => {
    make();
    const off = onPhonePaired((id) => nav.go({ name: 'chat', cid: id }));
    return () => {
      off();
      stopHostPairing();
    };
  }, [make, nav]);

  const expired = typeof code === 'object' && code !== null && now > code.expires;
  const qrSize = Math.min(width - 64, 300);

  let body;
  if (code === 'loading') {
    body = <ActivityIndicator color={t.accent} style={{ height: qrSize }} />;
  } else if (code === null || code === 'error' || expired) {
    body = (
      <View style={[s.problem, { minHeight: qrSize }]}>
        <Icon name={code === null ? 'wifi-outline' : 'time-outline'} size={32} color={t.muted} />
        <Text style={[s.body, { color: t.text2 }]}>
          {code === null ? tr('host.noWifi') : code === 'error' ? tr('host.failed') : tr('host.expired')}
        </Text>
        <Button title={tr('host.newCode')} kind="secondary" icon="refresh" onPress={make} />
      </View>
    );
  } else {
    body = (
      <View style={{ alignItems: 'center', gap: 12 }}>
        <View style={s.qrFrame}>
          <QRCode value={code.url} size={qrSize} />
        </View>
        <Text style={[s.name, { color: t.text }]}>{deviceName()}</Text>
      </View>
    );
  }

  return (
    <View style={{ flex: 1 }}>
      <Header
        left={<BackButton onPress={nav.back} />}
        title={<Text style={[s.headTitle, { color: t.text, textAlign: dir.start }]}>{tr('host.title')}</Text>}
      />
      <ScrollView contentContainerStyle={{ padding: 16, gap: 14, paddingBottom: insets.bottom + 16 }}>
        <Text style={[s.body, { color: t.text2, textAlign: dir.start }]}>{tr('host.intro')}</Text>
        <Card t={t} style={{ alignItems: 'center', paddingVertical: 20 }}>
          {body}
        </Card>
        <Text style={[s.note, { color: t.muted, textAlign: dir.start }]}>{tr('host.note')}</Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  headTitle: { fontSize: 16, fontWeight: '600' },
  body: { fontSize: 14, lineHeight: 20 },
  note: { fontSize: 13, lineHeight: 18 },
  name: { fontSize: 15, fontWeight: '600' },
  qrFrame: { borderRadius: 12, overflow: 'hidden' },
  problem: { alignItems: 'center', justifyContent: 'center', gap: 12, paddingHorizontal: 12 },
});
