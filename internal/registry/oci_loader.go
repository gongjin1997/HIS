package registry

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"

	"github.com/kinstall/kinstall/internal/artifact/oci"
	"github.com/kinstall/kinstall/internal/artifact/verify"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// LoadDeclarativeFromOCI extracts plugin.yaml from a signed OCI 1.1 artifact layout via oras-go/v2,
// verifies its SHA-256 and optional offline SM2/SM3 signature, and enforces the 15-task sandbox (T-M5-21, ADR-044/069).
func LoadDeclarativeFromOCI(ctx context.Context, layoutDir, tag, expectedSHA256 string, sm2Pub *ecdsa.PublicKey) (sdk.Component, error) {
	yamlBytes, _, err := oci.ExtractFile(ctx, layoutDir, tag, "plugin.yaml")
	if err != nil {
		return nil, fmt.Errorf("extract plugin.yaml from oci %s:%s: %w", layoutDir, tag, err)
	}
	if expectedSHA256 != "" {
		if err := verify.VerifySHA256(bytes.NewReader(yamlBytes), expectedSHA256); err != nil {
			return nil, err
		}
	}
	if sm2Pub != nil {
		sigBytes, _, serr := oci.ExtractFile(ctx, layoutDir, tag, "plugin.yaml.sm2.sig")
		if serr != nil {
			return nil, fmt.Errorf("missing SM2 signature in oci plugin %s:%s: %w", layoutDir, tag, serr)
		}
		if err := verify.VerifySM2(sm2Pub, yamlBytes, sigBytes); err != nil {
			return nil, err
		}
	}
	return LoadDeclarativeYAML(yamlBytes)
}
