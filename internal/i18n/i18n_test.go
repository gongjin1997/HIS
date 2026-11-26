package i18n_test

import (
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/i18n"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestCatalogBilingual(t *testing.T) {
	cat, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	zh := cat.Translate("zh-CN", sdk.CodeSrcOfflineStrictDenied, nil)
	en := cat.Translate("en-US", sdk.CodeSrcOfflineStrictDenied, nil)
	if !strings.Contains(zh, "offlineStrict") || !strings.Contains(zh, "离线") {
		t.Fatalf("unexpected zh translation: %q", zh)
	}
	if !strings.Contains(en, "offlineStrict") || !strings.Contains(en, "denied") {
		t.Fatalf("unexpected en translation: %q", en)
	}
	sug := cat.Translate("en-US", sdk.CodeExecHostKeyMismatch+".suggestion", nil)
	if !strings.Contains(sug, "accept-hostkey") {
		t.Fatalf("unexpected suggestion: %q", sug)
	}
}
