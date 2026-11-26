package template_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/kinstall/kinstall/internal/template"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestDeterministicRenderAndURLEncodeDualRedaction(t *testing.T) {
	eng := template.New(func(_ context.Context, name string) (string, error) {
		if name == "ddsPass" {
			return "P@ss/w0rd#2026%", nil
		}
		return "", nil
	})
	var registered []string
	tpl := sdk.TemplateRef{Inline: `dsn=mongodb://app:{{ urlEncode (secret "ddsPass") }}@{{ .Host }}:{{ add .Port 100 }}`}

	var first []byte
	for i := 0; i < 10; i++ {
		out, err := eng.Render(context.Background(), tpl, map[string]any{"Host": "10.0.1.5", "Port": 27000}, func(s string) {
			registered = append(registered, s)
		})
		if err != nil {
			t.Fatalf("run %d render failed: %v", i, err)
		}
		if i == 0 {
			first = out
		} else if !bytes.Equal(first, out) {
			t.Fatalf("non-deterministic output on run %d", i)
		}
	}
	want := "dsn=mongodb://app:P%40ss%2Fw0rd%232026%25@10.0.1.5:27100"
	if string(first) != want {
		t.Fatalf("got %q, want %q", string(first), want)
	}
	if len(registered) < 2 {
		t.Fatalf("expected raw and url-encoded secret registered, got %v", registered)
	}

	// Missing key must fail deterministically with KI-TPL-0001
	if _, err := eng.Render(context.Background(), sdk.TemplateRef{Inline: "{{ .Missing }}"}, map[string]any{}, nil); !sdk.IsCode(err, sdk.CodeTemplateRenderFailed) {
		t.Fatalf("expected KI-TPL-0001 on missing key, got %v", err)
	}

	// Safe expr evaluation
	ok, err := eng.EvalBool(`os.family == "rhel-like" && memMB >= 4096`, map[string]any{
		"os":    map[string]any{"family": "rhel-like"},
		"memMB": 8192,
	})
	if err != nil || !ok {
		t.Fatalf("expr eval failed: ok=%v err=%v", ok, err)
	}
}
