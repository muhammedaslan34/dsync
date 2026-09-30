// Colors from the desktop app (frontend/src/style.css), light and dark.
import { Platform, useColorScheme } from 'react-native';

export interface Theme {
  dark: boolean;
  bg: string;
  panel: string;
  raised: string;
  border: string;
  borderStrong: string;
  text: string;
  text2: string;
  muted: string;
  accent: string;
  accentSoft: string;
  accentText: string;
  bubbleIn: string;
  bubbleOut: string;
  bubbleOutText: string;
  bubbleOutMuted: string;
  online: string;
  offline: string;
  danger: string;
  dangerSoft: string;
  ok: string;
  hover: string;
  avatarS: number;
  avatarLBg: number;
  avatarLFg: number;
}

export const light: Theme = {
  dark: false,
  bg: '#f7f7f9',
  panel: '#ffffff',
  raised: '#ffffff',
  border: '#e3e3ea',
  borderStrong: '#cfcfd9',
  text: '#17171c',
  text2: '#4a4a57',
  muted: '#74747f',
  accent: '#5b5bd6',
  accentSoft: '#ececfd',
  accentText: '#ffffff',
  bubbleIn: '#ffffff',
  bubbleOut: '#5b5bd6',
  bubbleOutText: '#ffffff',
  bubbleOutMuted: 'rgba(255,255,255,0.72)',
  online: '#22a55b',
  offline: '#b4b4bf',
  danger: '#d93a3a',
  dangerSoft: '#fdecec',
  ok: '#1f8f50',
  hover: 'rgba(20,20,40,0.05)',
  avatarS: 65,
  avatarLBg: 90,
  avatarLFg: 36,
};

export const dark: Theme = {
  ...light,
  dark: true,
  bg: '#131316',
  panel: '#19191d',
  raised: '#202025',
  border: '#2a2a31',
  borderStrong: '#3a3a43',
  text: '#ececf1',
  text2: '#c2c2cc',
  muted: '#8d8d99',
  accent: '#7373e8',
  accentSoft: '#25254a',
  bubbleIn: '#222228',
  bubbleOut: '#4b4bc6',
  offline: '#55555f',
  danger: '#f06a6a',
  dangerSoft: '#3a1d20',
  ok: '#4cc47f',
  hover: 'rgba(255,255,255,0.06)',
  avatarS: 45,
  avatarLBg: 24,
  avatarLFg: 80,
};

export function useTheme(): Theme {
  return useColorScheme() === 'dark' ? dark : light;
}

export const mono = Platform.select({ ios: 'Menlo', android: 'monospace', default: 'monospace' });

export const radius = 10;
