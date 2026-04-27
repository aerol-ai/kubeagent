package upgrade

import "testing"

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		current   string
		want      bool
	}{
		{name: "newer semver", candidate: "0.2.0", current: "0.1.9", want: true},
		{name: "same semver different prefix", candidate: "v0.2.0", current: "0.2.0", want: false},
		{name: "older semver", candidate: "0.1.0", current: "0.2.0", want: false},
		{name: "invalid current falls back to upgrade", candidate: "0.3.0", current: "latest", want: true},
		{name: "invalid candidate skipped", candidate: "latest", current: "0.2.0", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNewerVersion(tc.candidate, tc.current); got != tc.want {
				t.Fatalf("IsNewerVersion(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}

func TestParseImageReference(t *testing.T) {
	tests := []struct {
		name      string
		imageRepo string
		wantHost  string
		wantRepo  string
	}{
		{name: "ghcr reference", imageRepo: "ghcr.io/aerol-ai/kubeagent", wantHost: "ghcr.io", wantRepo: "aerol-ai/kubeagent"},
		{name: "docker hub library", imageRepo: "nginx", wantHost: defaultRegistry, wantRepo: "library/nginx"},
		{name: "docker hub namespace", imageRepo: "aerol/kubeagent:1.2.3", wantHost: defaultRegistry, wantRepo: "aerol/kubeagent"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := parseImageReference(tc.imageRepo)
			if err != nil {
				t.Fatalf("parseImageReference(%q) returned error: %v", tc.imageRepo, err)
			}
			if ref.Registry != tc.wantHost || ref.Repository != tc.wantRepo {
				t.Fatalf("parseImageReference(%q) = %#v, want host=%q repo=%q", tc.imageRepo, ref, tc.wantHost, tc.wantRepo)
			}
		})
	}
}

func TestImageTag(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{image: "ghcr.io/aerol-ai/kubeagent:0.1.6", want: "0.1.6"},
		{image: "ghcr.io/aerol-ai/kubeagent:v0.1.6", want: "v0.1.6"},
		{image: "ghcr.io/aerol-ai/kubeagent@sha256:deadbeef", want: ""},
	}

	for _, tc := range tests {
		if got := imageTag(tc.image); got != tc.want {
			t.Fatalf("imageTag(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}
