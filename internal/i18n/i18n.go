package i18n

import (
	"embed"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Catalog wraps an embedded go-i18n bundle supporting zh-CN (default) and en-US (ADR-038).
type Catalog struct {
	bundle *goi18n.Bundle
}

// New constructs a new i18n Catalog from the embedded TOML message files.
func New() (*Catalog, error) {
	b := goi18n.NewBundle(language.SimplifiedChinese)
	b.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	for _, name := range []string{"locales/active.zh-CN.toml", "locales/active.en-US.toml"} {
		if _, err := b.LoadMessageFileFS(localeFS, name); err != nil {
			return nil, err
		}
	}
	return &Catalog{bundle: b}, nil
}

// Translate resolves a message key (such as "KI-EXEC-0002" or "KI-EXEC-0002.suggestion")
// for the given Accept-Language / CLI lang preference list.
func (c *Catalog) Translate(langHeader, msgID string, args map[string]any) string {
	if c == nil || c.bundle == nil {
		return msgID
	}
	langs := parseLangs(langHeader)
	loc := goi18n.NewLocalizer(c.bundle, langs...)
	out, err := loc.Localize(&goi18n.LocalizeConfig{
		MessageID:    msgID,
		TemplateData: args,
	})
	if err != nil || out == "" {
		return msgID
	}
	return out
}

// ResolveCLILang implements 05 §5.4 priority: --lang > KINSTALL_LANG > LC_ALL/LANG > zh-CN.
func ResolveCLILang(flagLang string) string {
	for _, candidate := range []string{
		flagLang,
		os.Getenv("KINSTALL_LANG"),
		os.Getenv("LC_ALL"),
		os.Getenv("LANG"),
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if idx := strings.IndexByte(candidate, '.'); idx > 0 {
			candidate = candidate[:idx]
		}
		candidate = strings.ReplaceAll(candidate, "_", "-")
		if strings.HasPrefix(strings.ToLower(candidate), "en") {
			return "en-US"
		}
		if strings.HasPrefix(strings.ToLower(candidate), "zh") {
			return "zh-CN"
		}
	}
	return "zh-CN"
}

func parseLangs(header string) []string {
	header = strings.TrimSpace(header)
	if header == "" {
		return []string{"zh-CN", "en-US"}
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts)+2)
	for _, p := range parts {
		if idx := strings.IndexByte(p, ';'); idx >= 0 {
			p = p[:idx]
		}
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	out = append(out, "zh-CN", "en-US")
	return out
}
