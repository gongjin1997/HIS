package verify

import (
	"encoding/hex"
	"fmt"
	"net/http"

	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// LoadSigstoreTrustedRoot parses a sigstore-go v1.4.1 offline trusted_root.json bundle with strict nil/empty defense
// (guards against kyverno#18525 panic, D3 §1.3, T-M1-10).
func LoadSigstoreTrustedRoot(raw []byte) (*sigroot.TrustedRoot, error) {
	if len(raw) == 0 {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false,
			fmt.Errorf("empty sigstore trusted_root.json"))
	}
	tr, err := sigroot.NewTrustedRootFromJSON(raw)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false,
			fmt.Errorf("parse sigstore trusted_root.json: %w", err))
	}
	if tr == nil {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false,
			fmt.Errorf("nil sigstore trusted root"))
	}
	return tr, nil
}

// VerifySigstoreBundleOffline verifies a Sigstore .sigstore.json bundle completely offline against trusted_root.json
// and the artifact's SHA-256 digest (zero outbound network calls, T-M1-10).
func VerifySigstoreBundleOffline(trustedRootJSON, bundleJSON []byte, artifactSHA256Hex, issuerRegex, subjectRegex string) (*sigverify.VerificationResult, error) {
	tr, err := LoadSigstoreTrustedRoot(trustedRootJSON)
	if err != nil {
		return nil, err
	}
	digest, err := hex.DecodeString(artifactSHA256Hex)
	if err != nil || len(digest) != 32 {
		return nil, sdk.NewError(sdk.CodeSrcMissingSHA256, "ART", http.StatusUnprocessableEntity, sdk.CodeSrcMissingSHA256, false,
			fmt.Errorf("invalid sha256 digest %q", artifactSHA256Hex))
	}
	var b sigbundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return nil, sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false,
			fmt.Errorf("parse sigstore bundle: %w", err))
	}

	sev, err := sigverify.NewVerifier(tr,
		sigverify.WithSignedCertificateTimestamps(1),
		sigverify.WithTransparencyLog(1),
		sigverify.WithObserverTimestamps(1),
	)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false, err)
	}

	var idOpt sigverify.PolicyOption
	if issuerRegex != "" || subjectRegex != "" {
		certID, cerr := sigverify.NewShortCertificateIdentity("", issuerRegex, "", subjectRegex)
		if cerr != nil {
			return nil, sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false, cerr)
		}
		idOpt = sigverify.WithCertificateIdentity(certID)
	} else {
		idOpt = sigverify.WithoutIdentitiesUnsafe()
	}

	res, err := sev.Verify(&b, sigverify.NewPolicy(sigverify.WithArtifactDigest("sha256", digest), idOpt))
	if err != nil {
		return nil, sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false,
			fmt.Errorf("offline sigstore verification failed: %w", err))
	}
	return res, nil
}
