import './src/random';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, BackHandler, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { loadComputers, useStore } from './src/store';
import { useTheme } from './src/theme';
import ComputersScreen from './src/screens/ComputersScreen';
import ScanScreen from './src/screens/ScanScreen';
import ChatScreen from './src/screens/ChatScreen';
import SettingsScreen from './src/screens/SettingsScreen';

export type Route =
  | { name: 'list' }
  | { name: 'scan' }
  | { name: 'chat'; cid: string }
  | { name: 'settings'; cid: string };

export type Nav = {
  go: (r: Route) => void;
  back: () => void;
};

function parent(r: Route): Route | null {
  switch (r.name) {
    case 'list':
      return null;
    case 'settings':
      return { name: 'chat', cid: r.cid };
    default:
      return { name: 'list' };
  }
}

export default function App() {
  const t = useTheme();
  const ready = useStore((s) => s.ready);
  const [route, setRoute] = useState<Route>({ name: 'list' });

  useEffect(() => {
    void loadComputers();
  }, []);

  const back = useCallback(() => {
    setRoute((r) => parent(r) ?? r);
  }, []);

  useEffect(() => {
    const sub = BackHandler.addEventListener('hardwareBackPress', () => {
      if (route.name === 'list') return false;
      back();
      return true;
    });
    return () => sub.remove();
  }, [route, back]);

  const nav: Nav = { go: setRoute, back };

  let screen;
  if (!ready) {
    screen = (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <ActivityIndicator color={t.accent} />
      </View>
    );
  } else if (route.name === 'scan') {
    screen = <ScanScreen nav={nav} />;
  } else if (route.name === 'chat') {
    screen = <ChatScreen key={route.cid} cid={route.cid} nav={nav} />;
  } else if (route.name === 'settings') {
    screen = <SettingsScreen cid={route.cid} nav={nav} />;
  } else {
    screen = <ComputersScreen nav={nav} />;
  }

  return (
    <SafeAreaProvider>
      <StatusBar style={t.dark ? 'light' : 'dark'} />
      <View style={{ flex: 1, backgroundColor: t.bg }}>{screen}</View>
    </SafeAreaProvider>
  );
}
