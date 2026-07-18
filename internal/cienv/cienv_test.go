package cienv

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		name    string
		actions string
		tfBuild string
		want    Kind
	}{
		{"local run", "", "", None},
		{"github actions", "true", "", GitHubActions},
		{"azure pipelines", "", "True", AzurePipelines},
		// Azure documents "True"; accept any casing of either marker.
		{"azure lowercase", "", "true", AzurePipelines},
		{"github uppercase", "TRUE", "", GitHubActions},
		// Non-boolean junk must not count as detection.
		{"junk values", "1", "yes", None},
		// Both set never happens on a real runner; GitHub wins, stably.
		{"both set", "true", "True", GitHubActions},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_ACTIONS", tc.actions)
			t.Setenv("TF_BUILD", tc.tfBuild)
			if got := Detect(); got != tc.want {
				t.Errorf("Detect() = %v, want %v", got, tc.want)
			}
		})
	}
}
