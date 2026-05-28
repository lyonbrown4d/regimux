package admin

import (
	"embed"
	"encoding/json"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/samber/oops"
)

const (
	localeEN = "en"
	localeZH = "zh"

	languageCookie = "regimux_admin_lang"
)

//go:embed locales/*.json
var localeFS embed.FS

var (
	messagesOnce sync.Once
	messages     map[string]map[string]string
	messagesErr  error
)

func loadMessages() (map[string]map[string]string, error) {
	messagesOnce.Do(func() {
		messages = map[string]map[string]string{}
		for _, locale := range []string{localeEN, localeZH} {
			content, err := localeFS.ReadFile("locales/" + locale + ".json")
			if err != nil {
				messagesErr = oops.In("admin").With("locale", locale).Wrapf(err, "read admin locale")
				return
			}
			entries := map[string]string{}
			if err := json.Unmarshal(content, &entries); err != nil {
				messagesErr = oops.In("admin").With("locale", locale).Wrapf(err, "parse admin locale")
				return
			}
			messages[locale] = entries
		}
	})
	return messages, messagesErr
}

func localeFromRequest(c *fiber.Ctx) string {
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

func translate(locale, key string) string {
	loaded, err := loadMessages()
	if err != nil {
		return key
	}
	if localized, ok := loaded[locale][key]; ok {
		return localized
	}
	if english, ok := loaded[localeEN][key]; ok {
		return english
	}
	return key
}

func templateTranslate(root any, key string) string {
	switch data := root.(type) {
	case PageData:
		return translate(data.Locale, key)
	case *PageData:
		if data != nil {
			return translate(data.Locale, key)
		}
	}
	return translate(localeEN, key)
}
