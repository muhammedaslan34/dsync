// Languages: English, Arabic, Turkish and French.
//
// t('chat.placeholder', { name }) looks a message up in the current language
// (falling back to English), fills in {placeholders} and picks plural forms
// with Intl.PluralRules. useI18n() re-renders a component when the language
// changes; t() can also be called outside React (for example in describeError).
//
// Right-to-left: the app lays itself out by hand from `isRTL` (see
// useDirection) instead of I18nManager.forceRTL, which needs a restart and
// behaves badly in Expo Go. The root view pins Yoga to LTR, so the native
// RTL flag can't flip the layout a second time.

import { useSyncExternalStore } from 'react';
import { getCalendars, getLocales } from 'expo-localization';
import type { IconName } from './components/ui';
import en, { type Dict, type Plural } from './locales/en';
import ar from './locales/ar';
import tr from './locales/tr';
import fr from './locales/fr';
import * as storage from './storage';

export const LANGS = ['en', 'ar', 'tr', 'fr'] as const;
export type Lang = (typeof LANGS)[number];
export type LangSetting = 'system' | Lang;

/** Each language's name, written in that language. */
export const LANG_NAMES: Record<Lang, string> = { en: 'English', ar: 'العربية', tr: 'Türkçe', fr: 'Français' };

const dicts: Record<Lang, Dict> = { en, ar, tr, fr };
const RTL_LANGS: ReadonlySet<Lang> = new Set<Lang>(['ar']);

// ---------- keys ----------

type Leaves<T, P extends string = ''> = {
  [K in keyof T & string]: T[K] extends string
    ? `${P}${K}`
    : T[K] extends readonly string[]
      ? never
      : T[K] extends Plural
        ? `${P}${K}`
        : Leaves<T[K], `${P}${K}.`>;
}[keyof T & string];

/** A message key, like 'chat.send'. */
export type MsgKey = Leaves<Dict>;
export type Vars = Record<string, string | number>;

// ---------- state ----------

export interface I18n {
  setting: LangSetting;
  lang: Lang;
  /** BCP 47 tag for Intl (the device's own tag when it's in the same language, e.g. fr-CA). */
  locale: string;
  isRTL: boolean;
  /** From the device clock setting, or undefined for the language's default. */
  hour12: boolean | undefined;
  /** The language "System" resolves to. */
  systemLang: Lang;
  t: (key: MsgKey, vars?: Vars) => string;
  dict: Dict;
}

function systemLang(): { lang: Lang; tag: string | null } {
  try {
    for (const l of getLocales()) {
      const code = (l.languageCode ?? l.languageTag.split('-')[0]).toLowerCase() as Lang;
      if (LANGS.includes(code)) return { lang: code, tag: l.languageTag };
    }
  } catch {
    // no locale info: English
  }
  return { lang: 'en', tag: null };
}

function deviceHour12(): boolean | undefined {
  try {
    const c = getCalendars()[0];
    if (c && typeof c.uses24hourClock === 'boolean') return !c.uses24hourClock;
  } catch {}
  return undefined;
}

function build(setting: LangSetting): I18n {
  const sys = systemLang();
  const lang = setting === 'system' ? sys.lang : setting;
  // Use the device's regional variant (en-GB, fr-CA, ar-EG) when it's in this language.
  let locale: string = lang;
  try {
    const dev = getLocales().find((l) => (l.languageCode ?? '').toLowerCase() === lang);
    if (dev?.languageTag) locale = dev.languageTag;
  } catch {}
  const next: I18n = {
    setting,
    lang,
    locale,
    isRTL: RTL_LANGS.has(lang),
    hour12: deviceHour12(),
    systemLang: sys.lang,
    dict: dicts[lang],
    t: (key, vars) => translate(next, key, vars),
  };
  return next;
}

let current: I18n = build('system');
const listeners = new Set<() => void>();

function set(next: I18n) {
  current = next;
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** The current language state (outside React). */
export function getI18n(): I18n {
  return current;
}

/** Loads the saved language choice; call once at startup. */
export async function loadLanguage() {
  const saved = await storage.loadLanguage();
  const setting: LangSetting = saved && (saved === 'system' || LANGS.includes(saved as Lang)) ? (saved as LangSetting) : 'system';
  set(build(setting));
}

/** Switches language right away and remembers the choice. */
export function setLanguage(setting: LangSetting) {
  set(build(setting));
  void storage.saveLanguage(setting);
}

/** Re-reads the device language (after it changed while the app was running). */
export function refreshSystemLanguage() {
  const next = build(current.setting);
  if (next.lang !== current.lang || next.locale !== current.locale || next.hour12 !== current.hour12 || next.systemLang !== current.systemLang) {
    set(next);
  }
}

/** The language state; the component re-renders when it changes. */
export function useI18n(): I18n {
  return useSyncExternalStore(subscribe, getI18n);
}

/** Translates with the current language (outside React). */
export function t(key: MsgKey, vars?: Vars): string {
  return current.t(key, vars);
}

// ---------- translating ----------

function lookup(d: Dict, key: string): string | Plural | undefined {
  let v: unknown = d;
  for (const part of key.split('.')) {
    if (v == null || typeof v !== 'object') return undefined;
    v = (v as Record<string, unknown>)[part];
  }
  if (typeof v === 'string') return v;
  if (v && typeof v === 'object' && typeof (v as Plural).other === 'string') return v as Plural;
  return undefined;
}

// Unicode bidi controls: first-strong isolate, pop isolate, right-to-left mark.
const FSI = '⁨';
const PDI = '⁩';
const RLM = '‏';

/** Whether the first letter with a strong direction is left-to-right. */
function startsLTR(s: string): boolean {
  for (const ch of s) {
    if (/[֐-ࣿיִ-﷿ﹰ-﻿]/.test(ch)) return false;
    if (/[A-Za-zÀ-ɏ]/.test(ch)) return true;
  }
  return false;
}

function translate(i: I18n, key: MsgKey, vars?: Vars): string {
  let msg = lookup(i.dict, key) ?? lookup(en, key) ?? key;
  if (typeof msg !== 'string') msg = msg[pluralCategory(i, Number(vars?.count ?? 0))] ?? msg.other;
  let out = msg;
  if (vars) {
    out = msg.replace(/\{(\w+)\}/g, (m, name: string) => {
      if (!(name in vars)) return m;
      const v = String(vars[name]);
      // In Arabic, keep names, addresses and file names in their own direction.
      return i.isRTL ? FSI + v + PDI : v;
    });
  }
  // An Arabic sentence that starts with "dsync" still reads right to left.
  if (i.isRTL && startsLTR(msg)) out = RLM + out;
  return out;
}

// ---------- plurals ----------

const pluralRules = new Map<string, { select(n: number): string } | null>();

/** Intl.PluralRules where the engine has it (Hermes may not), else the CLDR rules by hand. */
function pluralCategory(i: I18n, n: number): keyof Plural {
  let pr = pluralRules.get(i.lang);
  if (pr === undefined) {
    pr = null;
    try {
      if (typeof Intl !== 'undefined' && typeof Intl.PluralRules === 'function') pr = new Intl.PluralRules(i.lang);
    } catch {}
    pluralRules.set(i.lang, pr);
  }
  if (pr) {
    try {
      return pr.select(n) as keyof Plural;
    } catch {}
  }
  const abs = Math.abs(n);
  switch (i.lang) {
    case 'ar': {
      const m = abs % 100;
      if (abs === 0) return 'zero';
      if (abs === 1) return 'one';
      if (abs === 2) return 'two';
      if (m >= 3 && m <= 10) return 'few';
      if (m >= 11 && m <= 99) return 'many';
      return 'other';
    }
    case 'fr':
      return abs < 2 ? 'one' : 'other';
    default:
      return abs === 1 ? 'one' : 'other';
  }
}

// ---------- direction ----------

export interface Direction {
  isRTL: boolean;
  /** flexDirection for a row that reads from the start. */
  row: 'row' | 'row-reverse';
  /** textAlign for the start and the end of the line. */
  start: 'left' | 'right';
  end: 'left' | 'right';
  /** The back chevron, pointing towards the start. */
  backIcon: IconName;
  /** A chevron pointing forwards (towards the end). */
  forwardIcon: IconName;
}

const LTR: Direction = { isRTL: false, row: 'row', start: 'left', end: 'right', backIcon: 'chevron-back', forwardIcon: 'chevron-forward' };
const RTL: Direction = { isRTL: true, row: 'row-reverse', start: 'right', end: 'left', backIcon: 'chevron-forward', forwardIcon: 'chevron-back' };

/** Layout helpers for the current reading direction. */
export function useDirection(): Direction {
  return useI18n().isRTL ? RTL : LTR;
}

/**
 * Turns Left/Right style keys into Start/End ones that follow the reading
 * direction: side(isRTL, { marginLeft: 6 }) is a margin on the start side.
 * Start/End (not Left/Right) because React Native may swap Left and Right
 * itself when the phone's system language is right-to-left.
 */
export function side<T extends Record<string, unknown>>(isRTL: boolean, style: T): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(style)) {
    let nk = k;
    if (k === 'left') nk = isRTL ? 'end' : 'start';
    else if (k === 'right') nk = isRTL ? 'start' : 'end';
    else if (k.includes('Left')) nk = k.replace('Left', isRTL ? 'End' : 'Start');
    else if (k.includes('Right')) nk = k.replace('Right', isRTL ? 'Start' : 'End');
    out[nk] = v;
  }
  return out;
}
