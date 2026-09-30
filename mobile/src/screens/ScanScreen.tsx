import { useRef, useState } from 'react';
import { KeyboardAvoidingView, Linking, Platform, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { CameraView, useCameraPermissions, type BarcodeScanningResult } from 'expo-camera';
import * as Clipboard from 'expo-clipboard';
import * as Device from 'expo-device';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import { describeError, pair, parsePairingUrl } from '../dsync';
import { addComputer } from '../store';
import { useTheme } from '../theme';
import { Button, Card, Header, Icon, IconButton } from '../components/ui';

type Phase = { kind: 'scanning' } | { kind: 'connecting'; name: string; address: string } | { kind: 'error'; message: string };

function phoneName(): string {
  return Device.deviceName || Device.modelName || (Platform.OS === 'ios' ? 'iPhone' : 'Android phone');
}

export default function ScanScreen({ nav }: { nav: Nav }) {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const [permission, requestPermission] = useCameraPermissions();
  const [phase, setPhase] = useState<Phase>({ kind: 'scanning' });
  const [pasted, setPasted] = useState('');
  const busy = useRef(false);

  const connect = async (raw: string) => {
    if (busy.current) return;
    busy.current = true;
    try {
      const code = parsePairingUrl(raw);
      setPhase({ kind: 'connecting', name: code.name, address: code.addrs[0] });
      const computer = await pair(
        code,
        { name: phoneName(), platform: Platform.OS === 'ios' ? 'ios' : 'android' },
        (address) => setPhase({ kind: 'connecting', name: code.name, address }),
      );
      await addComputer(computer);
      nav.go({ name: 'chat', cid: computer.id });
    } catch (e) {
      setPhase({ kind: 'error', message: describeError(e) });
    } finally {
      busy.current = false;
    }
  };

  const onScanned = (r: BarcodeScanningResult) => {
    if (phase.kind !== 'scanning' || busy.current) return;
    void connect(r.data);
  };

  let camera;
  if (!permission) {
    camera = <View style={[s.camera, { backgroundColor: '#000' }]} />;
  } else if (!permission.granted) {
    camera = (
      <Card t={t} style={s.permCard}>
        <View style={[s.permIcon, { backgroundColor: t.accentSoft }]}>
          <Icon name="camera-outline" size={28} color={t.accent} />
        </View>
        <Text style={[s.title, { color: t.text }]}>Camera needed to scan</Text>
        <Text style={[s.body, { color: t.muted }]}>
          dsync uses the camera only to read the pairing code on your computer's screen.
        </Text>
        {permission.canAskAgain ? (
          <Button title="Allow camera" onPress={() => void requestPermission()} style={{ alignSelf: 'stretch' }} />
        ) : (
          <Button title="Open settings" kind="secondary" onPress={() => void Linking.openSettings()} style={{ alignSelf: 'stretch' }} />
        )}
        <Text style={[s.body, { color: t.muted, marginTop: 4 }]}>Or paste the pairing link below.</Text>
      </Card>
    );
  } else {
    camera = (
      <View style={s.camera}>
        <CameraView
          style={StyleSheet.absoluteFill}
          facing="back"
          barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
          onBarcodeScanned={phase.kind === 'scanning' ? onScanned : undefined}
        />
        <View style={s.frameWrap} pointerEvents="none">
          <View style={s.frame} />
        </View>
      </View>
    );
  }

  return (
    <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding">
      <Header
        left={<IconButton icon="chevron-back" label="Back" onPress={nav.back} color={t.text} />}
        title={<Text style={[s.headTitle, { color: t.text }]}>Connect a computer</Text>}
      />
      <ScrollView contentContainerStyle={{ padding: 16, gap: 14, paddingBottom: insets.bottom + 16 }} keyboardShouldPersistTaps="handled">
        <Text style={[s.body, { color: t.text2, textAlign: 'left' }]}>
          On your computer, open dsync → Settings → Connect a phone, and point the camera at the code.
        </Text>

        {camera}

        {phase.kind === 'connecting' ? (
          <Card t={t} style={s.status}>
            <Button title={`Connecting to ${phase.name}…`} kind="ghost" busy onPress={() => {}} />
            <Text style={[s.small, { color: t.muted }]}>Trying {phase.address}</Text>
          </Card>
        ) : null}

        {phase.kind === 'error' ? (
          <View style={[s.error, { backgroundColor: t.dangerSoft }]}>
            <Icon name="alert-circle" size={20} color={t.danger} />
            <Text style={[s.errorText, { color: t.danger }]}>{phase.message}</Text>
          </View>
        ) : null}
        {phase.kind === 'error' ? (
          <Button title="Scan again" kind="secondary" icon="refresh" onPress={() => setPhase({ kind: 'scanning' })} />
        ) : null}

        <Text style={[s.label, { color: t.muted }]}>OR PASTE THE PAIRING LINK</Text>
        <View style={[s.inputRow, { backgroundColor: t.raised, borderColor: t.borderStrong }]}>
          <TextInput
            value={pasted}
            onChangeText={setPasted}
            placeholder="dsync://pair?…"
            placeholderTextColor={t.muted}
            autoCapitalize="none"
            autoCorrect={false}
            style={[s.input, { color: t.text }]}
          />
          <IconButton
            icon="clipboard-outline"
            label="Paste"
            onPress={async () => setPasted((await Clipboard.getStringAsync()).trim())}
          />
        </View>
        <Button
          title="Connect"
          kind="secondary"
          disabled={!pasted.trim() || phase.kind === 'connecting'}
          onPress={() => void connect(pasted)}
        />
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  headTitle: { fontSize: 16, fontWeight: '600' },
  camera: { height: 300, borderRadius: 16, overflow: 'hidden', backgroundColor: '#000' },
  frameWrap: { position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, alignItems: 'center', justifyContent: 'center' },
  frame: { width: 210, height: 210, borderRadius: 20, borderWidth: 3, borderColor: 'rgba(255,255,255,0.9)' },
  permCard: { alignItems: 'center', gap: 10, paddingVertical: 22 },
  permIcon: { width: 56, height: 56, borderRadius: 28, alignItems: 'center', justifyContent: 'center' },
  title: { fontSize: 16, fontWeight: '600' },
  body: { fontSize: 14, lineHeight: 20, textAlign: 'center' },
  small: { fontSize: 12.5, textAlign: 'center' },
  status: { paddingVertical: 8, gap: 0 },
  error: { flexDirection: 'row', gap: 10, padding: 12, borderRadius: 12, alignItems: 'flex-start' },
  errorText: { flex: 1, fontSize: 14, lineHeight: 20, fontWeight: '500' },
  label: { fontSize: 11, fontWeight: '600', letterSpacing: 0.7, marginTop: 6 },
  inputRow: { flexDirection: 'row', alignItems: 'center', borderWidth: 1, borderRadius: 12, paddingLeft: 12, paddingRight: 4 },
  input: { flex: 1, height: 46, fontSize: 14 },
});
