// SPDX-License-Identifier: MIT

package action

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestParseUsesAgainstRunner replays testdata/uses.json: for each line of
// testdata/uses.txt, what the runner's own converter makes of it as the
// "uses" of a composite step (gen/runner).
func TestParseUsesAgainstRunner(t *testing.T) {
	data, err := os.ReadFile("testdata/uses.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]runnerAnswer
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	lines, _ := os.ReadFile("testdata/uses.txt")
	n := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(lines), "\n"), "\n") {
		w, ok := want[line]
		if !ok {
			t.Errorf("%q: not in uses.json", line)
			continue
		}
		n++
		u, err := ParseUses(line)
		if len(w.Errors) > 0 {
			if err == nil {
				t.Errorf("%q: accepted as %+v, the runner says %q", line, u, w.Errors[0])
				continue
			}
			// "u.yml (Line: 6, Col: 13): <message>"
			_, msg, _ := strings.Cut(w.Errors[0], "): ")
			if err.Error() != msg {
				t.Errorf("%q: %q, the runner says %q", line, err.Error(), msg)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v, the runner accepts it", line, err)
			continue
		}
		ref := w.Execution.Steps[0].Reference
		checkReference(t, 0, u, ref)
	}
	if n < 60 {
		t.Errorf("only %d values compared", n)
	}
}

func TestParseUses(t *testing.T) {
	tests := []struct {
		in   string
		want Uses
		err  string
	}{
		{in: "actions/checkout@v4", want: Uses{Kind: UsesRepository, Owner: "actions", Repo: "checkout", Name: "actions/checkout", Ref: "v4"}},
		{in: "actions/aws/ec2@main", want: Uses{Kind: UsesRepository, Owner: "actions", Repo: "aws", Name: "actions/aws", Path: "ec2", Ref: "main"}},
		{in: `a\b\c@v1`, want: Uses{Kind: UsesRepository, Owner: "a", Repo: "b", Name: "a/b", Path: "c", Ref: "v1"}},
		{in: "/a//b/@c", want: Uses{Kind: UsesRepository, Owner: "a", Repo: "b", Name: "a/b", Ref: "c"}},
		{in: "owner/repo/.github/workflows/ci.yml@main", want: Uses{Kind: UsesRepository, Owner: "owner", Repo: "repo", Name: "owner/repo", Path: ".github/workflows/ci.yml", Ref: "main"}},
		{in: "./local/path", want: Uses{Kind: UsesLocal, Path: "./local/path"}},
		{in: `.\local\path`, want: Uses{Kind: UsesLocal, Path: `.\local\path`}},
		{in: "docker://alpine:3.8", want: Uses{Kind: UsesDocker, Image: "alpine:3.8"}},
		{in: "docker://gcr.io/my-project/my-image:latest", want: Uses{Kind: UsesDocker, Image: "gcr.io/my-project/my-image:latest"}},
		{in: "$/", want: Uses{Kind: UsesSelfRepository}},
		{in: "$//a/b", want: Uses{Kind: UsesSelfRepository, Path: "a/b"}},
		{in: "", err: "Expected format {org}/{repo}[/path]@ref. Actual ''"},
		{in: "actions/checkout", err: "Expected format {org}/{repo}[/path]@ref. Actual 'actions/checkout'"},
		{in: "actions/checkout@", err: "Expected format {org}/{repo}[/path]@ref. Actual 'actions/checkout@'"},
		{in: "checkout@v4", err: "Expected format {org}/{repo}[/path]@ref. Actual 'checkout@v4'"},
		{in: "/repo@v4", err: "Expected format {org}/{repo}[/path]@ref. Actual '/repo@v4'"},
		{in: "owner/@v4", err: "Expected format {org}/{repo}[/path]@ref. Actual 'owner/@v4'"},
		{in: "a/b@c@d", err: "Expected format {org}/{repo}[/path]@ref. Actual 'a/b@c@d'"},
		{in: "Docker://alpine", err: "Expected format {org}/{repo}[/path]@ref. Actual 'Docker://alpine'"},
		{in: "$/foo@v1", err: "Self-repository references do not support an '@ref' suffix. Actual '$/foo@v1'"},
	}
	for _, tc := range tests {
		got, err := ParseUses(tc.in)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("ParseUses(%q): err %v, want %q", tc.in, err, tc.err)
			}
			var ue *UsesError
			if err != nil && (!asUsesError(err, &ue) || ue.Value != tc.in) {
				t.Errorf("ParseUses(%q): error %#v does not carry the value", tc.in, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseUses(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
}

func asUsesError(err error, target **UsesError) bool {
	ue, ok := err.(*UsesError)
	*target = ue
	return ok
}

func TestParseImage(t *testing.T) {
	tests := []struct {
		in   string
		want Image
		err  string
	}{
		{in: "Dockerfile", want: Image{Kind: ImageDockerfile, Path: "Dockerfile"}},
		{in: "images/Dockerfile", want: Image{Kind: ImageDockerfile, Path: "images/Dockerfile"}},
		{in: "dockerfile", want: Image{Kind: ImageDockerfile, Path: "dockerfile"}},
		{in: "a/Dockerfile.prod", want: Image{Kind: ImageDockerfile, Path: "a/Dockerfile.prod"}},
		{in: "a/prod.Dockerfile", want: Image{Kind: ImageDockerfile, Path: "a/prod.Dockerfile"}},
		{in: "docker://ubuntu:18.04", want: Image{Kind: ImageRegistry, Ref: "ubuntu:18.04"}},
		{in: "Docker://alpine", want: Image{Kind: ImageRegistry, Ref: "alpine"}},
		{in: "docker://my/Dockerfile", want: Image{Kind: ImageRegistry, Ref: "my/Dockerfile"}},
		{in: "alpine:3", err: "'alpine:3' should be either '[path]/Dockerfile' or 'docker://image[:tag]'."},
		{in: "Dockerfile/readme", err: "'Dockerfile/readme' should be either '[path]/Dockerfile' or 'docker://image[:tag]'."},
		{in: "", err: "'' should be either '[path]/Dockerfile' or 'docker://image[:tag]'."},
	}
	for _, tc := range tests {
		got, err := ParseImage(tc.in)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("ParseImage(%q): err %v, want %q", tc.in, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseImage(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
}
