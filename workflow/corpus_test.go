// SPDX-License-Identifier: MIT

package workflow

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The tests in this file check SkipCI, MatchPush and Parse against the
// scenarios and the recordings of ermite-conformance. They are opt-in: set
// ERMITE_CORPUS to the directory that holds corpus/ and recordings/ (a
// checkout of ermite-conformance). The files are only read.
//
//	ERMITE_CORPUS=/path/to/ermite-conformance go test ./workflow -run Corpus -v

type corpusPush struct {
	Kind     string   `yaml:"kind"`
	Tree     string   `yaml:"tree"`
	Remove   []string `yaml:"remove"`
	Message  string   `yaml:"message"`
	Count    int      `yaml:"count"`
	Generate []struct {
		Path  string `yaml:"path"`
		Count int    `yaml:"count"`
	} `yaml:"generate"`
}

type corpusScenario struct {
	Pushes []corpusPush `yaml:"pushes"`
}

type corpusRecording struct {
	Pushes []struct {
		Index int `json:"index"`
		Runs  []struct {
			Workflow   string            `json:"workflow"`
			Conclusion string            `json:"conclusion"`
			Jobs       []json.RawMessage `json:"jobs"`
		} `json:"runs"`
	} `json:"pushes"`
}

// readTree returns the files under dir by path relative to dir.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestCorpusPushRules(t *testing.T) {
	root := os.Getenv("ERMITE_CORPUS")
	if root == "" {
		t.Skip("ERMITE_CORPUS is not set")
	}
	dirs, err := filepath.Glob(filepath.Join(root, "corpus", "*", "scenario.yml"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no scenario in %s/corpus: %v", root, err)
	}
	checked := 0
	for _, sc := range dirs {
		dir := filepath.Dir(sc)
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			var s corpusScenario
			b, err := os.ReadFile(sc)
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(b, &s); err != nil {
				t.Fatal(err)
			}
			for _, p := range s.Pushes {
				if !slices.Contains([]string{"branch", "tag", "tags", "delete"}, p.Kind) {
					t.Skipf("push kind %q is not modelled", p.Kind)
				}
			}
			rb, err := os.ReadFile(filepath.Join(root, "recordings", name+".json"))
			if err != nil {
				t.Skipf("no recording: %v", err)
			}
			var rec corpusRecording
			if err := json.Unmarshal(rb, &rec); err != nil {
				t.Fatal(err)
			}
			checked++
			checkScenario(t, dir, name, s, rec)
		})
	}
	if checked == 0 {
		t.Error("no scenario was checked")
	}
}

func checkScenario(t *testing.T, dir, name string, s corpusScenario, rec corpusRecording) {
	state := map[string]string{} // the files of the head commit
	for i, p := range s.Pushes {
		n := i + 1
		var changed []string
		if p.Kind != "delete" {
			tree := p.Tree
			if tree == "" && i == 0 {
				tree = "tree"
			}
			next := cloneFiles(state)
			if tree != "" {
				for f, c := range readTree(t, filepath.Join(dir, tree)) {
					next[f] = c
				}
			}
			for _, f := range p.Remove {
				delete(next, f)
			}
			for _, g := range p.Generate {
				for k := 1; k <= g.Count; k++ {
					next[strings.ReplaceAll(g.Path, "{n}", fmt.Sprint(k))] = "generated"
				}
			}
			for f, c := range next {
				if old, ok := state[f]; !ok || old != c {
					changed = append(changed, f)
				}
			}
			for f := range state {
				if _, ok := next[f]; !ok {
					changed = append(changed, f)
				}
			}
			slices.Sort(changed)
			state = next
		}
		message := p.Message
		if message == "" {
			message = fmt.Sprintf("scenario %s push %d", name, n)
		}

		info := PushInfo{Created: i == 0 && p.Kind == "branch", ChangedFiles: changed}
		refs := 1
		switch p.Kind {
		case "branch":
			info.Ref = "refs/heads/scn/" + name + "/r1"
		case "tag", "tags":
			refs = max(p.Count, 1)
			info.Ref = "refs/tags/scn-" + name + "-r1-1"
			info.TagCount = refs
			info.Created = true
		case "delete":
			// Deletes the last ref pushed.
			prev := s.Pushes[i-1]
			info.Deleted = true
			if prev.Kind == "branch" {
				info.Ref = "refs/heads/scn/" + name + "/r1"
			} else {
				info.Ref = "refs/tags/scn-" + name + "-r1-1"
				info.TagCount = 1
			}
		}

		got := map[string]int{} // runs by workflow file in the recording
		var failed []string     // workflows recorded as a failed run without job
		for _, rp := range rec.Pushes {
			if rp.Index != n {
				continue
			}
			for _, r := range rp.Runs {
				got[r.Workflow]++
				if r.Conclusion == "failure" && len(r.Jobs) == 0 {
					failed = append(failed, r.Workflow)
				}
			}
		}

		skip := p.Kind != "delete" && SkipCI(message)
		var files []string
		for f := range state {
			if strings.HasPrefix(f, ".github/workflows/") && path.Dir(f) == ".github/workflows" {
				files = append(files, f)
			}
		}
		slices.Sort(files)
		for _, f := range files {
			w, errs := Parse(f, []byte(state[f]))
			want := 0
			switch {
			case p.Kind == "delete" || skip:
			case len(errs) > 0:
				// An invalid file gives one failed run without job.
				want = 1
				if !slices.Contains(failed, f) {
					t.Errorf("push %d (%q): %s is invalid (%v) but the recording has no failed run for it", n, message, f, errs[0])
				}
			case w.MatchPush(info):
				want = refs
			}
			if got[f] != want {
				t.Errorf("push %d (%q, %s): %s: GitHub started %d run(s), we expect %d", n, message, p.Kind, f, got[f], want)
			}
		}
		for f, c := range got {
			if !slices.Contains(files, f) {
				t.Errorf("push %d: recording has %d run(s) of %s, which is not in the scenario", n, c, f)
			}
		}
	}
}

func cloneFiles(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
