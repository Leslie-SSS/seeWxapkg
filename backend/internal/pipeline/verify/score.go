package verify

import "github.com/keepbuild/seewxapkg/internal/domain/task"

func ComputeRecoveryScore(manifest *ManifestVerifyResult, artifacts *ArtifactVerifyResult, decompileRequested, fallbackUsed bool) *task.RecoveryScore {
	score := &task.RecoveryScore{
		DecompileHit: decompileRequested,
		FallbackUsed: fallbackUsed,
	}

	if manifest != nil {
		if manifest.PageCount == 0 {
			score.Manifest = 0
		} else {
			issues := manifest.ManifestIssueCount
			// Results created before ManifestIssueCount was introduced still need a
			// conservative score when missing pages are present.
			issues = max(issues, len(manifest.MissingPages)+len(manifest.InvalidTabBarPages))
			issues = min(issues, manifest.PageCount)
			score.Manifest = ratio(manifest.PageCount-issues, manifest.PageCount)
			if !manifest.Success && issues == 0 {
				// A failed manifest rule that is not represented by a page count must
				// never retain a perfect score.
				score.Manifest = min(score.Manifest, 80)
			}
		}
	}

	if artifacts != nil {
		if artifacts.TotalPages == 0 {
			score.JS, score.WXML, score.WXSS = 0, 0, 0
		} else {
			score.JS = ratio(artifacts.JSParseable, max(artifacts.TotalPages, artifacts.JSFiles))
			score.WXML = ratio(artifacts.WXMLParseable, max(artifacts.TotalPages, artifacts.WXMLFiles))
			if artifacts.WXMLQualityIssueFiles > 0 {
				qualityTotal := max(artifacts.TotalPages, artifacts.WXMLFiles)
				cleanFiles := max(artifacts.WXMLFiles-min(artifacts.WXMLQualityIssueFiles, artifacts.WXMLFiles), 0)
				score.WXML = min(score.WXML, ratio(cleanFiles, qualityTotal))
			}
			if artifacts.WXSSFiles == 0 {
				// A page without a wxss file is valid in Mini Programs. Absence must
				// not reduce the recovery score; existing style files still must parse.
				score.WXSS = 100
			} else {
				score.WXSS = ratio(artifacts.WXSSParseable, artifacts.WXSSFiles)
			}
		}
		if artifacts.TotalPages > 0 {
			missingRatio := ratio(len(artifacts.MissingPageTriplet), artifacts.TotalPages)
			score.GeneratedRatio = max(missingRatio, 0)
		}
	}
	score.VerifierPassed = manifest != nil && manifest.Success && artifacts != nil && artifacts.Success && artifacts.VerifierPassed

	if fallbackUsed {
		score.FallbackPenalty = 10
	}
	verifierWeight := 0
	if score.VerifierPassed {
		verifierWeight = 100
	}
	overall := float64(score.Manifest)*0.3 + float64(score.JS)*0.2 + float64(score.WXML)*0.2 + float64(score.WXSS)*0.2 + float64(verifierWeight)*0.1
	score.Overall = max(int(overall)-score.FallbackPenalty-score.GeneratedRatio/4, 0)
	if !score.VerifierPassed {
		// A report with a failed verification gate must not look like an
		// excellent recovery merely because the remaining files parse.
		score.Overall = min(score.Overall, 79)
	}
	return score
}

func ratio(ok, total int) int {
	if total <= 0 {
		return 0
	}
	return int(float64(ok) / float64(total) * 100)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
