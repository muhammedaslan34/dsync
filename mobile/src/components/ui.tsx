// Small shared pieces: avatar, buttons, header, hooks.
import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from 'react';
import { ActivityIndicator, AppState, Pressable, StyleSheet, Text, View, type StyleProp, type ViewStyle } from 'react-native';
import Ionicons from '@expo/vector-icons/Ionicons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { hueFor, initials } from '../format';
import { type Theme, useTheme } from '../theme';

export type IconName = ComponentProps<typeof Ionicons>['name'];

export function Icon({ name, size = 20, color }: { name: IconName; size?: number; color: string }) {
  return <Ionicons name={name} size={size} color={color} />;
}

export function Avatar({ name, id, size = 40, online, ringColor }: { name: string; id: string; size?: number; online?: boolean | null; ringColor?: string }) {
  const t = useTheme();
  const hue = hueFor(id || name);
  const dot = Math.round(size * 0.3);
  return (
    <View
      style={{
        width: size,
        height: size,
        borderRadius: size / 2,
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: `hsl(${hue}, ${t.avatarS}%, ${t.avatarLBg}%)`,
      }}
    >
      <Text style={{ color: `hsl(${hue}, ${t.avatarS}%, ${t.avatarLFg}%)`, fontSize: size * 0.38, fontWeight: '700', letterSpacing: 0.3 }}>
        {initials(name)}
      </Text>
      {online !== undefined && online !== null ? (
        <View
          style={{
            position: 'absolute',
            right: -1,
            bottom: -1,
            width: dot,
            height: dot,
            borderRadius: dot / 2,
            borderWidth: 2,
            borderColor: ringColor ?? t.bg,
            backgroundColor: online ? t.online : t.offline,
          }}
        />
      ) : null}
    </View>
  );
}

export function Logo({ size = 30 }: { size?: number }) {
  return (
    <View
      style={{
        width: size,
        height: size,
        borderRadius: size * 0.3,
        backgroundColor: '#5b5bd6',
        alignItems: 'center',
        justifyContent: 'center',
        shadowColor: '#4b4bc6',
        shadowOpacity: 0.35,
        shadowRadius: 6,
        shadowOffset: { width: 0, height: 2 },
        elevation: 3,
      }}
    >
      <Ionicons name="swap-horizontal" size={size * 0.58} color="#fff" />
    </View>
  );
}

type BtnKind = 'primary' | 'secondary' | 'danger' | 'ghost';

export function Button({
  title,
  onPress,
  kind = 'primary',
  icon,
  busy,
  disabled,
  style,
}: {
  title: string;
  onPress: () => void;
  kind?: BtnKind;
  icon?: IconName;
  busy?: boolean;
  disabled?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const t = useTheme();
  const colors: Record<BtnKind, { bg: string; fg: string; border: string }> = {
    primary: { bg: t.accent, fg: t.accentText, border: t.accent },
    secondary: { bg: t.raised, fg: t.text, border: t.borderStrong },
    danger: { bg: t.dangerSoft, fg: t.danger, border: t.dangerSoft },
    ghost: { bg: 'transparent', fg: t.accent, border: 'transparent' },
  };
  const c = colors[kind];
  const off = disabled || busy;
  return (
    <Pressable
      onPress={onPress}
      disabled={off}
      style={({ pressed }) => [
        styles.btn,
        { backgroundColor: c.bg, borderColor: c.border, opacity: off ? 0.5 : pressed ? 0.8 : 1 },
        style,
      ]}
    >
      {busy ? <ActivityIndicator size="small" color={c.fg} /> : icon ? <Ionicons name={icon} size={18} color={c.fg} /> : null}
      <Text style={[styles.btnText, { color: c.fg }]}>{title}</Text>
    </Pressable>
  );
}

export function IconButton({
  icon,
  onPress,
  color,
  size = 22,
  label,
  disabled,
  style,
}: {
  icon: IconName;
  onPress: () => void;
  color?: string;
  size?: number;
  label: string;
  disabled?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const t = useTheme();
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityLabel={label}
      hitSlop={6}
      style={({ pressed }) => [styles.iconBtn, { backgroundColor: pressed ? t.hover : 'transparent', opacity: disabled ? 0.4 : 1 }, style]}
    >
      <Ionicons name={icon} size={size} color={color ?? t.text2} />
    </Pressable>
  );
}

export function Header({ left, title, right }: { left?: ReactNode; title: ReactNode; right?: ReactNode }) {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.header, { paddingTop: insets.top + 8, backgroundColor: t.panel, borderBottomColor: t.border }]}>
      {left}
      <View style={{ flex: 1, minWidth: 0 }}>{title}</View>
      {right}
    </View>
  );
}

export function Card({ children, style, t }: { children: ReactNode; style?: StyleProp<ViewStyle>; t: Theme }) {
  return <View style={[styles.card, { backgroundColor: t.raised, borderColor: t.border }, style]}>{children}</View>;
}

/** Current time, updated every `ms` (for online dots). */
export function useNow(ms = 1000): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(id);
  }, [ms]);
  return now;
}

/** Whether the app is in the foreground. */
export function useForeground(): boolean {
  const [active, setActive] = useState(AppState.currentState === 'active');
  useEffect(() => {
    const sub = AppState.addEventListener('change', (s) => setActive(s === 'active'));
    return () => sub.remove();
  }, []);
  return active;
}

/** Runs fn now and every `ms` while enabled; never overlaps itself. */
export function useInterval(fn: () => Promise<unknown> | void, ms: number, enabled: boolean) {
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => {
    if (!enabled) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      try {
        await ref.current();
      } catch {
        // errors are recorded by fn
      }
      if (!stopped) timer = setTimeout(tick, ms);
    };
    void tick();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    };
  }, [ms, enabled]);
}

export const styles = StyleSheet.create({
  btn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingHorizontal: 16,
    height: 46,
    borderRadius: 12,
    borderWidth: 1,
  },
  btnText: { fontSize: 15, fontWeight: '600' },
  iconBtn: { width: 38, height: 38, borderRadius: 10, alignItems: 'center', justifyContent: 'center' },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingHorizontal: 10,
    paddingBottom: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  card: {
    borderRadius: 14,
    borderWidth: 1,
    padding: 16,
    shadowColor: '#141428',
    shadowOpacity: 0.06,
    shadowRadius: 2,
    shadowOffset: { width: 0, height: 1 },
    elevation: 1,
  },
});
