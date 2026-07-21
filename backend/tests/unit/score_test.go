package unit

import (
	"testing"

	"github.com/keepbuild/seewxapkg/internal/pipeline/verify"
)

func TestComputeRecoveryScoreAppliesPenalties(t *testing.T) {
	score := verify.ComputeRecoveryScore(
		&verify.ManifestVerifyResult{Success: true, PageCount: 10},
		&verify.ArtifactVerifyResult{
			Success:            false,
			VerifierPassed:     false,
			TotalPages:         10,
			PageTriplets:       6,
			JSFiles:            8,
			WXMLFiles:          7,
			WXSSFiles:          7,
			JSParseable:        8,
			WXMLParseable:      5,
			WXSSParseable:      4,
			MissingPageTriplet: []string{"a", "b", "c", "d"},
		},
		true,
		true,
	)

	if !score.FallbackUsed {
		t.Fatalf("expected fallback flag")
	}
	if score.FallbackPenalty == 0 {
		t.Fatalf("expected fallback penalty")
	}
	if score.VerifierPassed {
		t.Fatalf("expected verifier to fail")
	}
	if score.Overall >= 90 {
		t.Fatalf("expected overall score to be penalized, got %d", score.Overall)
	}
}

func TestComputeRecoveryScoreReflectsManifestAndWXMLQualityIssues(t *testing.T) {
	score := verify.ComputeRecoveryScore(
		&verify.ManifestVerifyResult{
			Success:            false,
			PageCount:          19,
			InvalidTabBarPages: []string{"pages/index/index.html", "pages/user/profile/index.html"},
			ManifestIssueCount: 2,
		},
		&verify.ArtifactVerifyResult{
			Success:               false,
			ParserPassed:          true,
			VerifierPassed:        false,
			TotalPages:            19,
			PageTriplets:          19,
			JSFiles:               100,
			JSParseable:           100,
			WXMLFiles:             34,
			WXMLParseable:         34,
			WXMLQualityIssueFiles: 31,
			WXSSFiles:             34,
			WXSSParseable:         34,
		},
		true,
		true,
	)

	if score.Manifest != 89 {
		t.Fatalf("manifest score = %d, want 89", score.Manifest)
	}
	if score.WXML > 10 {
		t.Fatalf("WXML quality gaps must materially affect the score, got %d", score.WXML)
	}
	if score.VerifierPassed || score.Overall > 60 {
		t.Fatalf("failed static verification is overstated: %#v", score)
	}
}
