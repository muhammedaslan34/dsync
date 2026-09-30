// Paired computers, kept in the phone's secure storage (Keychain / Keystore).
// One entry per computer plus an index, so each value stays small.
import * as SecureStore from 'expo-secure-store';
import type { Computer } from './dsync';

const INDEX_KEY = 'dsync.computers';

function entryKey(id: string): string {
  return 'dsync.computer.' + id.replace(/[^A-Za-z0-9._-]/g, '_');
}

async function readIndex(): Promise<string[]> {
  try {
    const raw = await SecureStore.getItemAsync(INDEX_KEY);
    const ids = raw ? JSON.parse(raw) : [];
    return Array.isArray(ids) ? ids.filter((x) => typeof x === 'string') : [];
  } catch {
    return [];
  }
}

export async function loadComputers(): Promise<Computer[]> {
  const ids = await readIndex();
  const out: Computer[] = [];
  for (const id of ids) {
    try {
      const raw = await SecureStore.getItemAsync(entryKey(id));
      if (!raw) continue;
      const c = JSON.parse(raw) as Computer;
      if (c && c.id && c.key && c.phoneId) out.push(c);
    } catch {
      // skip a broken entry
    }
  }
  return out;
}

export async function saveComputer(c: Computer): Promise<void> {
  await SecureStore.setItemAsync(entryKey(c.id), JSON.stringify(c));
  const ids = await readIndex();
  if (!ids.includes(c.id)) {
    ids.push(c.id);
    await SecureStore.setItemAsync(INDEX_KEY, JSON.stringify(ids));
  }
}

export async function removeComputer(id: string): Promise<void> {
  await SecureStore.deleteItemAsync(entryKey(id));
  const ids = (await readIndex()).filter((x) => x !== id);
  await SecureStore.setItemAsync(INDEX_KEY, JSON.stringify(ids));
}

// ---------- app settings ----------

const LANGUAGE_KEY = 'dsync.language';

/** The chosen language ('system', 'en', 'ar', ...), or null if never set. */
export async function loadLanguage(): Promise<string | null> {
  try {
    return await SecureStore.getItemAsync(LANGUAGE_KEY);
  } catch {
    return null;
  }
}

export async function saveLanguage(value: string): Promise<void> {
  try {
    await SecureStore.setItemAsync(LANGUAGE_KEY, value);
  } catch {
    // not saved: the choice still applies until the app restarts
  }
}
