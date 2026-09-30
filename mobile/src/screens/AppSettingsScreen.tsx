// The app's own settings: for now, the language.
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Nav } from '../../App';
import { LANGS, LANG_NAMES, type LangSetting, setLanguage, useDirection, useI18n } from '../i18n';
import { useTheme } from '../theme';
import { BackButton, Card, Header, Icon } from '../components/ui';

export default function AppSettingsScreen({ nav }: { nav: Nav }) {
  const t = useTheme();
  const i18n = useI18n();
  const dir = useDirection();
  const insets = useSafeAreaInsets();

  const options: { value: LangSetting; title: string; sub?: string }[] = [
    { value: 'system', title: i18n.t('app.system'), sub: LANG_NAMES[i18n.systemLang] },
    ...LANGS.map((l) => ({ value: l as LangSetting, title: LANG_NAMES[l] })),
  ];

  return (
    <View style={{ flex: 1 }}>
      <Header
        left={<BackButton onPress={nav.back} />}
        title={<Text style={[s.headTitle, { color: t.text, textAlign: dir.start }]}>{i18n.t('app.title')}</Text>}
      />
      <ScrollView contentContainerStyle={{ padding: 16, gap: 8, paddingBottom: insets.bottom + 24 }}>
        <Text style={[s.label, { color: t.muted, textAlign: dir.start }, dir.isRTL && s.labelArabic]}>{i18n.t('app.language')}</Text>
        <Card t={t} style={{ paddingVertical: 4, paddingHorizontal: 0 }}>
          {options.map((o, idx) => {
            const selected = i18n.setting === o.value;
            return (
              <Pressable
                key={o.value}
                onPress={() => setLanguage(o.value)}
                accessibilityRole="radio"
                accessibilityState={{ selected }}
                style={({ pressed }) => [
                  s.row,
                  { flexDirection: dir.row, backgroundColor: pressed ? t.hover : 'transparent' },
                  idx > 0 && { borderTopColor: t.border, borderTopWidth: StyleSheet.hairlineWidth },
                ]}
              >
                <View style={{ flex: 1, minWidth: 0 }}>
                  <Text style={[s.title, { color: t.text, textAlign: dir.start }]}>{o.title}</Text>
                  {o.sub ? <Text style={[s.sub, { color: t.muted, textAlign: dir.start }]}>{o.sub}</Text> : null}
                </View>
                {selected ? <Icon name="checkmark" size={20} color={t.accent} /> : <View style={{ width: 20 }} />}
              </Pressable>
            );
          })}
        </Card>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  headTitle: { fontSize: 16, fontWeight: '600' },
  label: { fontSize: 11, fontWeight: '600', letterSpacing: 0.7, marginHorizontal: 6 },
  labelArabic: { letterSpacing: 0, fontSize: 12 },
  row: { alignItems: 'center', gap: 12, paddingHorizontal: 16, paddingVertical: 12, minHeight: 50 },
  title: { fontSize: 15.5 },
  sub: { fontSize: 12.5, marginTop: 1 },
});
