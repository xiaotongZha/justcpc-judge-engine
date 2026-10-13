package regression_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"justcpc-judge-engine/src/judge"
	"justcpc-judge-engine/src/sandbox"
)

func TestReferenceSolutions(t *testing.T) {
	root := writableCgroupRoot(t)
	if root == "" {
		t.Skip("the current host does not provide a writable cgroup v2 subtree")
	}
	runner, err := sandbox.New(sandbox.Config{
		RootFS:     projectRoot(t, "runtime", "judge-rootfs"),
		WorkRoot:   t.TempDir(),
		CgroupRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := judge.NewFileSystemRepository(judge.FileSystemRepositoryConfig{
		Root: projectRoot(t, "testdata"),
		DefaultLimit: map[judge.Language]judge.ResourceLimit{
			judge.LanguageCPP:     {TimeLimitMS: 2000, MemoryLimitMB: 512},
			judge.LanguageJava:    {TimeLimitMS: 2000, MemoryLimitMB: 512},
			judge.LanguagePython3: {TimeLimitMS: 2000, MemoryLimitMB: 512},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := judge.NewEngine(runner, repository, nil, nil)
	for _, contest := range []string{"contest-17", "contest-18"} {
		references, err := os.ReadDir(projectRoot(t, "testdata", contest, "reference"))
		if err != nil {
			t.Fatal(err)
		}
		for _, reference := range references {
			if filepath.Ext(reference.Name()) != ".cpp" {
				continue
			}
			problem := strings.TrimSuffix(reference.Name(), ".cpp")
			t.Run(contest+"/"+problem, func(t *testing.T) {
				source, err := os.ReadFile(projectRoot(t, "testdata", contest, "reference", reference.Name()))
				if err != nil {
					t.Fatal(err)
				}
				response, err := engine.Judge(context.Background(), judge.JudgeTask{
					ProblemID: strings.TrimPrefix(contest, "contest-") + "/" + problem,
					Language:  judge.LanguageCPP,
					Source:    string(source),
				})
				if err != nil {
					t.Fatal(err)
				}
				if response.Verdict != judge.VerdictAccepted {
					t.Fatalf("verdict = %s; compile = %#v; failed cases = %s", response.Verdict, response.Compile, failures(response.Cases))
				}
				t.Logf("%d cases accepted", len(response.Cases))
			})
		}
	}
}

func failures(cases []judge.CaseResult) string {
	var failed []string
	for _, result := range cases {
		if result.Verdict != judge.VerdictAccepted {
			failed = append(failed, fmt.Sprintf("%s:%s(%s)", result.ID, result.Verdict, result.Error))
		}
	}
	return strings.Join(failed, ", ")
}

func writableCgroupRoot(t *testing.T) string {
	t.Helper()
	root, err := sandbox.CurrentCgroupRoot()
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "justcpc-regression-probe")
	if err := os.Mkdir(probe, 0755); err != nil {
		return ""
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	return root
}

func projectRoot(t *testing.T, elements ...string) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(append([]string{root}, elements...)...)
}
