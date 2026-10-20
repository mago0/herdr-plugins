package model

import "testing"

func TestIsProject(t *testing.T) {
	for link, want := range map[string]bool{
		"https://linear.app/flosports/project/gke-aws-eks-migration-f425468449cd": true,
		"https://linear.app/flosports/issue/SRE-194/migrate-job-scheduler":         false,
		"https://example.com/flosports/project/x":                                  false,
		"": false,
	} {
		if got := IsProject(link); got != want {
			t.Errorf("IsProject(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestPRNumber(t *testing.T) {
	for link, want := range map[string]string{
		"https://github.com/flocasts/iam/pull/57":       "#57",
		"https://github.com/flocasts/iam/pull/57/files": "#57",
		"https://github.com/flocasts/iam/issues/57":     "",
		"https://linear.app/flosports/issue/SRE-194":    "",
		"": "",
	} {
		if got := PRNumber(link); got != want {
			t.Errorf("PRNumber(%q) = %q, want %q", link, got, want)
		}
	}
}
