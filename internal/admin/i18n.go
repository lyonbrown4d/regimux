package admin

import (
	"embed"
	"encoding/json"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/gofiber/fiber/v3"
	"github.com/samber/oops"
)

const (
	localeEN = "en"
	localeZH = "zh"

	languageCookie = "regimux_admin_lang"
)

//go:embed locales/*.json
var localeFS embed.FS

type Messages struct {
	entries *collectionmapping.Table[string, string, string]
}

func NewMessages() (*Messages, error) {
	locales := collectionlist.NewList(localeEN, localeZH)
	loaded := collectionmapping.NewTable[string, string, string]()
	var loadErr error
	locales.Range(func(_ int, locale string) bool {
		entries, err := readLocaleMessages(locale)
		if err != nil {
			loadErr = err
			return false
		}
		loaded.SetRow(locale, entries)
		return true
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return &Messages{entries: loaded}, nil
}

func readLocaleMessages(locale string) (map[string]string, error) {
	content, err := localeFS.ReadFile("locales/" + locale + ".json")
	if err != nil {
		return nil, oops.In("admin").With("locale", locale).Wrapf(err, "read admin locale")
	}
	entries := map[string]string{}
	if err := json.Unmarshal(content, &entries); err != nil {
		return nil, oops.In("admin").With("locale", locale).Wrapf(err, "parse admin locale")
	}
	return entries, nil
}

func localeFromRequest(c fiber.Ctx) string {
	if c == nil {
		return localeEN
	}
	if locale, ok := normalizeLocale(c.Query("lang")); ok {
		c.Cookie(&fiber.Cookie{
			Name:     languageCookie,
			Value:    locale,
			Path:     basePath,
			MaxAge:   365 * 24 * 60 * 60,
			HTTPOnly: true,
			SameSite: "Lax",
		})
		return locale
	}
	if locale, ok := normalizeLocale(c.Cookies(languageCookie)); ok {
		return locale
	}
	return localeFromAcceptLanguage(c.Get("Accept-Language"))
}

func normalizeLocale(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(value, localeZH), strings.HasPrefix(value, "cn"):
		return localeZH, true
	case strings.HasPrefix(value, localeEN):
		return localeEN, true
	default:
		return "", false
	}
}

func localeFromAcceptLanguage(value string) string {
	for part := range strings.SplitSeq(value, ",") {
		language, _, _ := strings.Cut(part, ";")
		if locale, ok := normalizeLocale(language); ok {
			return locale
		}
	}
	return localeEN
}

func htmlLang(locale string) string {
	if locale == localeZH {
		return "zh-CN"
	}
	return localeEN
}

func oppositeLocale(locale string) string {
	if locale == localeZH {
		return localeEN
	}
	return localeZH
}

func (m *Messages) Translate(locale, key string) string {
	if m == nil {
		return key
	}
	if localized, ok := m.translateLocale(locale, key); ok {
		return localized
	}
	if english, ok := m.translateLocale(localeEN, key); ok {
		return english
	}
	return key
}

func (m *Messages) translateLocale(locale, key string) (string, bool) {
	if m == nil || m.entries == nil {
		return "", false
	}
	return m.entries.Get(locale, key)
}

func (m *Messages) TemplateTranslate(root any, key string) string {
	switch data := root.(type) {
	case PageData:
		return m.Translate(data.Locale, key)
	case *PageData:
		if data != nil {
			return m.Translate(data.Locale, key)
		}
	}
	return m.Translate(localeEN, key)
}
