package contract

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeScriptLanguage(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "", want: ScriptLangTypeScript},
		{input: "  ", want: ScriptLangTypeScript},
		{input: " typescript ", want: ScriptLangTypeScript},
		{input: "python", want: ScriptLangPython},
		{input: "go", want: ScriptLangGo},
	} {
		got, err := NormalizeScriptLanguage(test.input)
		if err != nil {
			t.Fatalf("NormalizeScriptLanguage(%q) returned error: %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("NormalizeScriptLanguage(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	if _, err := NormalizeScriptLanguage("ruby"); err == nil || !strings.Contains(err.Error(), "unsupported scriptLang") {
		t.Fatalf("NormalizeScriptLanguage(ruby) error = %v, want unsupported scriptLang", err)
	}
}

func TestExecutionProfileCanonicalCompatibility(t *testing.T) {
	bun, err := NewExecutionProfile("sha256:image-a", "linux", "amd64", "typescript", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	if bun.Runtime != ExecutionRuntimeBun {
		t.Fatalf("runtime = %q, want bun", bun.Runtime)
	}
	same, err := NewExecutionProfile("sha256:image-a", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	if !ExecutionProfilesCompatible(bun, same) {
		t.Fatal("equivalent TypeScript/Bun profiles are not compatible")
	}
	windows, err := NewExecutionProfile("sha256:image-a", "windows", "amd64", "bun", "1.2.3", "none")
	if err != nil {
		t.Fatal(err)
	}
	if ExecutionProfilesCompatible(bun, windows) {
		t.Fatal("Linux and Windows profiles must not be compatible")
	}
	musl, err := NewExecutionProfile("sha256:image-a", "linux", "amd64", "bun", "1.2.3", "musl-1.2.5")
	if err != nil {
		t.Fatal(err)
	}
	if ExecutionProfilesCompatible(bun, musl) {
		t.Fatal("glibc and musl profiles must not be compatible")
	}
	tampered := bun
	tampered.RuntimeABI = "9.9.9"
	if err := ValidateExecutionProfile(tampered); err == nil {
		t.Fatal("tampered profile key was accepted")
	}
	label, err := ExecutionProfileLabel(bun)
	if err != nil || !strings.HasPrefix(label, "sys/execution-profile-") {
		t.Fatalf("profile label = %q, err=%v", label, err)
	}
}

func TestValidAppKeyAcceptsLiteAndFCodeKeys(t *testing.T) {
	valid := []string{"greet", "a1", "my_app", "4MDCPCM", "CESTORE", "A1", "1greet"}
	invalid := []string{"", "a", " Greet", "greet ", "my-app", "my.app", "with space", "a/b", "\uD55C\uAE00"}
	for _, value := range valid {
		if !ValidAppKey(value) {
			t.Fatalf("ValidAppKey(%q) = false, want true", value)
		}
	}
	for _, value := range invalid {
		if ValidAppKey(value) {
			t.Fatalf("ValidAppKey(%q) = true, want false", value)
		}
	}
}

func TestValidActionKeyAcceptsLiteAndFCodeModuleKeys(t *testing.T) {
	valid := []string{"hello", "a", "approval.sync", "a.b.c.d.e.f.g.h", "sync_now", "1000", "M1000", "4MDCPCM.1000"}
	invalid := []string{"", " hello", "hello ", "a..b", ".a", "a.", "hel-lo", "a/b", `a\b`, "a.b.c.d.e.f.g.h.i", "\uD55C\uAE00"}
	for _, value := range valid {
		if !ValidActionKey(value) {
			t.Fatalf("ValidActionKey(%q) = false, want true", value)
		}
	}
	for _, value := range invalid {
		if ValidActionKey(value) {
			t.Fatalf("ValidActionKey(%q) = true, want false", value)
		}
	}
}

func TestEffectiveRouteTagPrecedence(t *testing.T) {
	appOverride := "app-blue"
	actionTag := "action-main"
	actionOverride := "action-fast"

	if got := EffectiveRouteTag("app-main", &appOverride, &actionTag, &actionOverride); got != "action-fast" {
		t.Fatalf("action override tag = %q, want action-fast", got)
	}
	if got := EffectiveRouteTag("app-main", &appOverride, &actionTag, nil); got != "app-blue" {
		t.Fatalf("app operator override = %q, want app-blue", got)
	}
	if got := EffectiveRouteTag("app-main", &appOverride, nil, nil); got != "app-blue" {
		t.Fatalf("app override tag = %q, want app-blue", got)
	}
	if got := EffectiveRouteTag("app-main", nil, nil, nil); got != "app-main" {
		t.Fatalf("app manifest tag = %q, want app-main", got)
	}
	if got := EffectiveRouteTag("", nil, nil, nil); got != DefaultRouteTag {
		t.Fatalf("default tag = %q, want %q", got, DefaultRouteTag)
	}
}

func TestNormalizeLabelsVocabulary(t *testing.T) {
	labels, err := NormalizeLabels([]string{"browser", "browser", "linux-arm64"}, false)
	if err != nil {
		t.Fatalf("NormalizeLabels returned error: %v", err)
	}
	if !reflect.DeepEqual(labels, []string{"browser", "linux-arm64"}) {
		t.Fatalf("labels = %#v", labels)
	}
	// Open vocabulary: anything well-formed is valid.
	if _, err := NormalizeLabels([]string{"gpu"}, false); err != nil {
		t.Fatalf("gpu must be a valid label now: %v", err)
	}
	for _, invalid := range []string{"", "UPPER", "-lead", "trail-", "has space", "a..b!"} {
		if _, err := NormalizeLabels([]string{invalid}, false); err == nil {
			t.Fatalf("label %q must be rejected", invalid)
		}
	}
	// sys/ is operator-reserved: rejected in manifests, allowed for workers.
	if _, err := NormalizeLabels([]string{"sys/pool.dedicated"}, false); err == nil {
		t.Fatal("sys/ labels must be rejected without allowReserved")
	}
	if _, err := NormalizeLabels([]string{"sys/pool.dedicated"}, true); err != nil {
		t.Fatalf("sys/ labels must be allowed for operators: %v", err)
	}
	many := make([]string, MaxLabels+1)
	for i := range many {
		many[i] = fmt.Sprintf("label-%02d", i)
	}
	if _, err := NormalizeLabels(many, false); err == nil {
		t.Fatalf("more than %d labels must be rejected", MaxLabels)
	}
}

func TestLabelsDoNotInfluenceRouteTags(t *testing.T) {
	deployment := Deployment{Tag: "default", RequiredCapabilities: []string{"browser"}, RequiredLabels: []string{"browser"}}
	if got := EffectiveRouteTagForAction(deployment, Action{}); got != "default" {
		t.Fatalf("route tag = %q, want default (labels are orthogonal)", got)
	}
}

func TestEffectiveRequiredLabels(t *testing.T) {
	deployment := Deployment{RequiredLabels: []string{"browser"}}
	runsOn := []string{"kr"}
	got := EffectiveRequiredLabels(deployment, Action{RunsOn: &runsOn})
	if !reflect.DeepEqual(got, []string{"browser", "kr"}) {
		t.Fatalf("effective labels = %#v, want union", got)
	}
	// Legacy deployments carry only requiredCapabilities.
	legacy := Deployment{RequiredCapabilities: []string{"browser"}}
	if got := EffectiveRequiredLabels(legacy, Action{}); !reflect.DeepEqual(got, []string{"browser"}) {
		t.Fatalf("legacy effective labels = %#v", got)
	}
}

func TestEffectiveRequiredLabelsOperatorPrecedence(t *testing.T) {
	appOverride := []string{"gpu"}
	actionOverride := []string{}
	actionManifest := []string{"browser"}
	deployment := Deployment{
		RequiredLabels:         []string{"linux"},
		RequiredLabelsOverride: &appOverride,
	}
	if got := EffectiveRequiredLabels(deployment, Action{RunsOn: &actionManifest}); !reflect.DeepEqual(got, []string{"gpu"}) {
		t.Fatalf("app override labels = %#v, want gpu", got)
	}
	if got := EffectiveRequiredLabels(deployment, Action{RunsOn: &actionManifest, RequiredLabelsOverride: &actionOverride}); len(got) != 0 {
		t.Fatalf("explicit action empty override = %#v, want none", got)
	}
}

func TestValidateSourceSubpathPreservesCanonicalRawPath(t *testing.T) {
	if err := ValidateSourceSubpath("apps/echo"); err != nil {
		t.Fatalf("ValidateSourceSubpath returned error: %v", err)
	}
}

func TestValidateSourceSubpathRejectsEscapingPaths(t *testing.T) {
	for _, subpath := range []string{"/apps/echo", "../apps/echo", "apps/../echo"} {
		if err := ValidateSourceSubpath(subpath); err == nil {
			t.Fatalf("ValidateSourceSubpath(%q) unexpectedly passed", subpath)
		}
	}
}

// The wf-family prefix contract lets fronting proxies classify engine-minted
// bearers they cannot verify. Every listed prefix must stay in the family,
// stay distinct, and end with an underscore separator.
func TestCellBearerTokenPrefixContract(t *testing.T) {
	prefixes := CellBearerTokenPrefixes()
	if len(prefixes) == 0 {
		t.Fatal("prefix contract must not be empty")
	}
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(prefix, "wf") {
			t.Fatalf("prefix %q leaves the wf family", prefix)
		}
		if !strings.HasSuffix(prefix, "_") {
			t.Fatalf("prefix %q must end with the _ separator", prefix)
		}
		if seen[prefix] {
			t.Fatalf("duplicate prefix %q", prefix)
		}
		seen[prefix] = true
		if !IsCellBearerToken(prefix + "payload") {
			t.Fatalf("IsCellBearerToken must accept %q tokens", prefix)
		}
	}
	if !seen[ClientTokenPrefix] {
		t.Fatalf("client token prefix %q is missing from the cell bearer contract", ClientTokenPrefix)
	}
	if IsCellBearerToken("imp_platform-token") {
		t.Fatal("platform namespace must not classify as a cell bearer")
	}
}
