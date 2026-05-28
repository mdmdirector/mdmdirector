package ddm

import "testing"

func TestClassifyDeclarationType(t *testing.T) {
	cases := []struct {
		declType        string
		wantClass       string
		wantSubtype     string
	}{
		{TypeLegacyProfile, DeclClassConfiguration, DeclSubtypeProfile},
		{TypePackage, DeclClassConfiguration, DeclSubtypeApplication},
		{TypeActivationSimple, DeclClassActivation, ""},
		{"com.apple.configuration.something-new", DeclClassConfiguration, ""},
		{"com.apple.activation.predicate", DeclClassActivation, ""},
		{"com.apple.asset.credential", DeclClassAsset, ""},
		{"com.apple.management.organization-info", DeclClassManagement, ""},
		{"com.apple.unknown.thing", DeclClassUnknown, ""},
		{"", DeclClassUnknown, ""},
	}
	for _, tc := range cases {
		gotClass, gotSubtype := ClassifyDeclarationType(tc.declType)
		if gotClass != tc.wantClass || gotSubtype != tc.wantSubtype {
			t.Errorf("ClassifyDeclarationType(%q) = (%q, %q), want (%q, %q)",
				tc.declType, gotClass, gotSubtype, tc.wantClass, tc.wantSubtype)
		}
	}
}

func TestClassifyDeclarationID(t *testing.T) {
	cases := []struct {
		identifier      string
		wantClass       string
		wantSubtype     string
	}{
		{LegacyProfileDeclarationID("com.example", "UDID-1", "com.profile.id"), DeclClassConfiguration, DeclSubtypeProfile},
		{ProfileActivationDeclarationID("com.example", "UDID-1", "com.profile.id"), DeclClassActivation, ""},
		{PackageDeclarationID("com.example", "UDID-1", "package-uuid"), DeclClassConfiguration, DeclSubtypeApplication},
		{PackageActivationDeclarationID("com.example", "UDID-1", "package-uuid"), DeclClassActivation, ""},
		{"some.unrelated.identifier", DeclClassUnknown, ""},
	}
	for _, tc := range cases {
		gotClass, gotSubtype := ClassifyDeclarationID(tc.identifier)
		if gotClass != tc.wantClass || gotSubtype != tc.wantSubtype {
			t.Errorf("ClassifyDeclarationID(%q) = (%q, %q), want (%q, %q)",
				tc.identifier, gotClass, gotSubtype, tc.wantClass, tc.wantSubtype)
		}
	}
}
