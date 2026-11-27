package stack

import "testing"

func TestInterpolateSpec(t *testing.T) {
	spec := map[string]any{
		"pgDSN":   "postgres://${instances.pg.outputs.endpoint}/his",
		"obsUrl":  "${externalRefs.obs.endpoint}/bucket",
		"obsCred": "${externalRefs.obs.credentialRef}",
		"nested": map[string]any{
			"brokers": []any{"${instances.kafka.outputs.bootstrapServers}"},
		},
	}
	upstream := map[string]map[string]any{
		"pg":    {"endpoint": "10.0.0.11:5432"},
		"kafka": {"bootstrapServers": "10.0.0.12:9092"},
	}
	extRefs := []ExternalRef{
		{ID: "obs", Kind: "obs", Endpoints: []string{"https://obs.internal:443"}, CredentialRef: "crd_obs_1"},
	}
	got := InterpolateSpec(spec, upstream, extRefs)
	if got["pgDSN"] != "postgres://10.0.0.11:5432/his" {
		t.Fatalf("unexpected pgDSN: %v", got["pgDSN"])
	}
	if got["obsUrl"] != "https://obs.internal:443/bucket" || got["obsCred"] != "crd_obs_1" {
		t.Fatalf("unexpected externalRef interpolation: %v / %v", got["obsUrl"], got["obsCred"])
	}
	nested := got["nested"].(map[string]any)["brokers"].([]any)
	if nested[0] != "10.0.0.12:9092" {
		t.Fatalf("unexpected nested interpolation: %v", nested[0])
	}
}
