package obs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/i18n"
)

// TestRepoErrorCodesConsistent scans all Go source files in the repository for KI-<DOMAIN>-<4DIGIT>
// literals and verifies every code is registered in both zh-CN and en-US i18n catalogs (05 §5.2).
func TestRepoErrorCodesConsistent(t *testing.T) {
	cat, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	reCode := regexp.MustCompile(`KI-[A-Z]+-[0-9]{4}`)
	root := filepath.Join("..", "..")
	seen := make(map[string]string)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "docs" || name == "web" || name == "node_modules" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, m := range reCode.FindAllString(string(b), -1) {
			seen[m] = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	for code, file := range seen {
		if cat.Translate("zh-CN", code, nil) == code {
			t.Errorf("error code %s (used in %s) missing zh-CN translation", code, file)
		}
		if cat.Translate("en-US", code, nil) == code {
			t.Errorf("error code %s (used in %s) missing en-US translation", code, file)
		}
	}
}
