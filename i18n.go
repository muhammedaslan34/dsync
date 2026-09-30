package main

import (
	"fmt"
	"strings"
)

// The few strings shown outside the window (tray menu, notifications, file
// dialog titles), by language. The window has its own translations in
// frontend/src/lib/locales; the wording here matches them.
var messages = map[string]map[string]string{
	"en": {
		"tray.open":        "Open dsync",
		"tray.openTip":     "Show the dsync window",
		"tray.clip":        "Sync clipboard",
		"tray.clipTip":     "Share the clipboard with paired devices",
		"tray.quit":        "Quit dsync",
		"tray.quitTip":     "Stop dsync completely",
		"tray.unread":      "dsync: new messages",
		"online.zero":      "No paired devices online",
		"online.one":       "1 device online",
		"online.other":     "%d devices online",
		"notify.running":   "dsync is still running",
		"notify.runningTx": "It keeps receiving in the background. Open it from the tray icon, or quit it there.",
		"notify.file":      "Sent you a file: %s",
		"notify.folder":    "Sent you a folder: %s",
		"dialog.files":     "Send files",
		"dialog.folder":    "Send folder",
		"dialog.receive":   "Save received files in",
	},
	"ar": {
		"tray.open":        "فتح dsync",
		"tray.openTip":     "إظهار نافذة dsync",
		"tray.clip":        "مزامنة الحافظة",
		"tray.clipTip":     "مشاركة الحافظة مع الأجهزة المقترنة",
		"tray.quit":        "إنهاء dsync",
		"tray.quitTip":     "إيقاف dsync تمامًا",
		"tray.unread":      "dsync: رسائل جديدة",
		"online.zero":      "لا أجهزة مقترنة متصلة",
		"online.one":       "جهاز واحد متصل",
		"online.two":       "جهازان متصلان",
		"online.few":       "%d أجهزة متصلة",
		"online.many":      "%d جهازًا متصلًا",
		"online.other":     "%d جهاز متصل",
		"notify.running":   "لا يزال dsync يعمل",
		"notify.runningTx": "يواصل الاستلام في الخلفية. افتحه من أيقونة شريط المهام، أو أنهِه من هناك.",
		"notify.file":      "أرسل إليك ملفًا: %s",
		"notify.folder":    "أرسل إليك مجلدًا: %s",
		"dialog.files":     "إرسال ملفات",
		"dialog.folder":    "إرسال مجلد",
		"dialog.receive":   "حفظ الملفات المستلمة في",
	},
	"tr": {
		"tray.open":        "dsync'i aç",
		"tray.openTip":     "dsync penceresini göster",
		"tray.clip":        "Panoyu eşitle",
		"tray.clipTip":     "Panoyu eşleştirilmiş cihazlarla paylaş",
		"tray.quit":        "dsync'ten çık",
		"tray.quitTip":     "dsync'i tamamen kapat",
		"tray.unread":      "dsync: yeni mesajlar",
		"online.zero":      "Çevrimiçi eşleştirilmiş cihaz yok",
		"online.other":     "%d cihaz çevrimiçi",
		"notify.running":   "dsync hâlâ çalışıyor",
		"notify.runningTx": "Arka planda almaya devam ediyor. Sistem tepsisindeki simgeden aç ya da oradan çık.",
		"notify.file":      "Sana bir dosya gönderdi: %s",
		"notify.folder":    "Sana bir klasör gönderdi: %s",
		"dialog.files":     "Dosya gönder",
		"dialog.folder":    "Klasör gönder",
		"dialog.receive":   "Alınan dosyaların kaydedileceği yer",
	},
	"fr": {
		"tray.open":        "Ouvrir dsync",
		"tray.openTip":     "Afficher la fenêtre de dsync",
		"tray.clip":        "Synchroniser le presse-papiers",
		"tray.clipTip":     "Partager le presse-papiers avec les appareils associés",
		"tray.quit":        "Quitter dsync",
		"tray.quitTip":     "Arrêter complètement dsync",
		"tray.unread":      "dsync : nouveaux messages",
		"online.zero":      "Aucun appareil associé en ligne",
		"online.one":       "1 appareil en ligne",
		"online.other":     "%d appareils en ligne",
		"notify.running":   "dsync est toujours lancé",
		"notify.runningTx": "Il continue de recevoir en arrière-plan. Ouvrez-le depuis l'icône de notification, ou quittez-le à partir de là.",
		"notify.file":      "Vous a envoyé un fichier : %s",
		"notify.folder":    "Vous a envoyé un dossier : %s",
		"dialog.files":     "Envoyer des fichiers",
		"dialog.folder":    "Envoyer un dossier",
		"dialog.receive":   "Enregistrer les fichiers reçus dans",
	},
}

// validLanguage reports whether lang is a language code dsync has.
func validLanguage(lang string) bool {
	_, ok := messages[lang]
	return ok
}

// language is the language in use: the one chosen in the settings, or the
// system's if dsync has it, or English.
func (a *App) language() string {
	if l := a.node.Language(); validLanguage(l) {
		return l
	}
	return matchLanguage(systemLocale())
}

// matchLanguage turns a locale such as "fr_FR.UTF-8" or "ar-SA" into one
// of dsync's languages, or English.
func matchLanguage(locale string) string {
	base := strings.ToLower(locale)
	if i := strings.IndexAny(base, "_-.@"); i >= 0 {
		base = base[:i]
	}
	if validLanguage(base) {
		return base
	}
	return "en"
}

// tr translates key, formatting args into it like fmt.Sprintf.
func (a *App) tr(key string, args ...any) string {
	return translate(a.language(), key, args...)
}

func translate(lang, key string, args ...any) string {
	s, ok := messages[lang][key]
	if !ok {
		s = messages["en"][key]
	}
	if len(args) > 0 && strings.Contains(s, "%") {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// devicesOnline is the tray's "3 devices online" line.
func devicesOnline(lang string, n int) string {
	if n == 0 {
		return translate(lang, "online.zero")
	}
	key := "online." + pluralCategory(lang, n)
	if _, ok := messages[lang][key]; !ok {
		key = "online.other"
	}
	return translate(lang, key, n)
}

// pluralCategory follows the CLDR rules for the counts dsync shows.
func pluralCategory(lang string, n int) string {
	switch lang {
	case "ar":
		switch m := n % 100; {
		case n == 1:
			return "one"
		case n == 2:
			return "two"
		case m >= 3 && m <= 10:
			return "few"
		case m >= 11 && m <= 99:
			return "many"
		}
		return "other"
	case "tr":
		return "other" // Turkish nouns stay singular after a number
	}
	if n == 1 {
		return "one"
	}
	return "other"
}
