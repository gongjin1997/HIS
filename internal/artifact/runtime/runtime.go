package runtime

import (
	"fmt"
	"net/http"
	"path"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// RootDir is the non-intrusive multi-version runtime root (/opt/kinstall/runtimes, D3 §4.1).
// Never modifies /usr/bin/java, /etc/profile, or bin/mvn!
const RootDir = "/opt/kinstall/runtimes"

// MinJDKCSPUDate blocks JDK 21/25 builds older than 2026-11-18 CSPU (CVE-2026-114938, D3 §4.2).
var MinJDKCSPUDate = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)

// JDKSpec describes a side-by-side JDK runtime installation.
type JDKSpec struct {
	Distribution string // temurin | bisheng
	Version      string // e.g. "21.0.10-cspu"
	Major        int    // 8 | 17 | 21 | 25
	ReleaseDate  time.Time
}

// ValidateJDKSecurity enforces the 2026-11-18 CSPU floor for JDK 21/25 (D3 §4.2).
func ValidateJDKSecurity(spec JDKSpec) error {
	if (spec.Major == 21 || spec.Major == 25) && !spec.ReleaseDate.IsZero() && spec.ReleaseDate.Before(MinJDKCSPUDate) {
		return sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false,
			fmt.Errorf("JDK %d (%s) released %s predates mandatory 2026-11-18 CSPU security floor (CVE-2026-114938)",
				spec.Major, spec.Version, spec.ReleaseDate.Format("2006-01-02")))
	}
	return nil
}

// JDKHome returns the isolated JAVA_HOME directory for a side-by-side JDK version.
func JDKHome(spec JDKSpec) string {
	dist := spec.Distribution
	if dist == "" {
		dist = "temurin"
	}
	return path.Join(RootDir, "jdk", fmt.Sprintf("%s-%s", dist, spec.Version))
}

// MavenHome returns the isolated Maven installation directory.
func MavenHome(version string) string {
	return path.Join(RootDir, "maven", version)
}

// NodeHome returns the isolated Node.js installation directory.
func NodeHome(version string) string {
	return path.Join(RootDir, "node", version)
}

// BuildCACertsInjectTask emits an idempotent cmd.run Task that imports a private CA into the isolated JDK cacerts (D3 §4.2).
func BuildCACertsInjectTask(jdkHome, caFile, caFP12 string) sdk.TaskSpec {
	alias := "kinstall-ca-" + caFP12
	check := fmt.Sprintf(`"%s/bin/keytool" -list -keystore "%s/lib/security/cacerts" -storepass changeit -alias "%s" >/dev/null 2>&1`, jdkHome, jdkHome, alias)
	apply := fmt.Sprintf(`cp -n "%s/lib/security/cacerts" "%s/lib/security/cacerts.kinstall.orig" 2>/dev/null || true
"%s/bin/keytool" -importcert -noprompt -trustcacerts -alias "%s" -file "%s" -keystore "%s/lib/security/cacerts" -storepass changeit`,
		jdkHome, jdkHome, jdkHome, alias, caFile, jdkHome)
	return sdk.CmdRun(apply, check)
}
