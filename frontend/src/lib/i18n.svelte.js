// Translations for the window. t() reads the current language from rune
// state, so everything that calls it (in markup or in $derived) updates as
// soon as the language changes.

import en from './locales/en.js'
import ar from './locales/ar.js'
import tr from './locales/tr.js'
import fr from './locales/fr.js'

const dicts = { en, ar, tr: tr, fr: frenchSpacing(fr) }

// The choices in the settings; each language is named in itself.
export const languages = [
  ['en', 'English'],
  ['ar', 'العربية'],
  ['tr', 'Türkçe'],
  ['fr', 'Français'],
]

const rtl = new Set(['ar'])

// First strong isolate and pop directional isolate: text between them
// keeps its own direction without reordering what's around it.
const FSI = String.fromCharCode(0x2068)
const PDI = String.fromCharCode(0x2069)

let state = $state({ pref: 'system', locale: 'en' })

export const i18n = {
  // What the user chose: 'system' or a language code.
  get pref() { return state.pref },
  // The language in use.
  get locale() { return state.locale },
  get rtl() { return rtl.has(state.locale) },
}

// systemLanguage is the first supported language the browser engine
// reports (the OS language), or English.
function systemLanguage() {
  for (const l of [navigator.language, ...(navigator.languages ?? [])]) {
    const base = l?.split('-')[0].toLowerCase()
    if (base && dicts[base]) return base
  }
  return 'en'
}

// setLanguage switches the window to pref ('system' or a language code).
export function setLanguage(pref) {
  if (!pref || !dicts[pref]) pref = 'system'
  state.pref = pref
  state.locale = pref === 'system' ? systemLanguage() : pref
  const root = document.documentElement
  root.lang = state.locale
  root.dir = rtl.has(state.locale) ? 'rtl' : 'ltr'
  try { localStorage.setItem('language', pref) } catch {}
}

// loadLanguage applies the language used last time, before the saved
// setting arrives from Go, so the window doesn't flash in English.
export function loadLanguage() {
  let pref = 'system'
  try { pref = localStorage.getItem('language') || 'system' } catch {}
  setLanguage(pref)
}

// intlLocale is the locale for Intl formatting: the system's own regional
// variant when it is the same language (so en-GB keeps a 24-hour clock),
// with Western digits for Arabic to match addresses, codes and file names.
let cachedTag = { key: '', tag: 'en' }
export function intlLocale() {
  const l = state.locale
  const nav = navigator.language || ''
  const key = l + '|' + nav
  if (cachedTag.key === key) return cachedTag.tag
  let tag = nav.toLowerCase().split('-')[0] === l ? nav : l
  if (l === 'ar') tag += '-u-nu-latn'
  try { Intl.getCanonicalLocales(tag) } catch { tag = l }
  cachedTag = { key, tag }
  return tag
}

const plurals = new Map()
function pluralRules() {
  const tag = intlLocale()
  if (!plurals.has(tag)) plurals.set(tag, new Intl.PluralRules(tag))
  return plurals.get(tag)
}

const numbers = new Map()
export function fmtNumber(n, digits = 0) {
  const key = intlLocale() + digits
  if (!numbers.has(key)) {
    numbers.set(key, new Intl.NumberFormat(intlLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits }))
  }
  return numbers.get(key).format(n)
}

// fmtList joins names the way the language does ("a, b and c").
export function fmtList(items) {
  try {
    return new Intl.ListFormat(intlLocale(), { type: 'conjunction' }).format(items)
  } catch {
    return items.join(', ')
  }
}

function lookup(dict, key) {
  let v = dict
  for (const part of key.split('.')) {
    v = v?.[part]
    if (v === undefined) return undefined
  }
  return v
}

// t translates key, filling {name}-style placeholders from vars. Numbers
// are formatted for the language; with a {count}, an entry written as
// { one, other, … } picks the plural form. In right-to-left languages,
// text values (names, file names, addresses) are isolated so their own
// direction can't reorder the sentence around them.
export function t(key, vars = {}) {
  let s = lookup(dicts[state.locale], key) ?? lookup(en, key)
  if (s === undefined) return key
  if (typeof s === 'object' && !Array.isArray(s)) {
    s = s[pluralRules().select(vars.count ?? 0)] ?? s.other
  }
  if (typeof s !== 'string') return s
  const isolate = rtl.has(state.locale)
  return s.replace(/\{(\w+)\}/g, (m, name) => {
    const v = vars[name]
    if (v === undefined || v === null) return m
    if (typeof v === 'number') return fmtNumber(v)
    return isolate ? FSI + v + PDI : String(v)
  })
}

// French puts a non-breaking space before ? ! : ; and inside « »; writing
// ordinary spaces in fr.js and fixing them here keeps that file readable.
function frenchSpacing(dict) {
  const fix = (v) => {
    if (typeof v === 'string') return v.replace(/ ([?!:;»])/g, ' $1').replace(/« /g, '« ')
    if (Array.isArray(v)) return v
    return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, fix(x)]))
  }
  return fix(dict)
}
