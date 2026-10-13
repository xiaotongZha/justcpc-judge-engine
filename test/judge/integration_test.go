package judge_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"justcpc-judge-engine/src/judge"
	"justcpc-judge-engine/src/sandbox"
)

func TestJudgeToolchainsEndToEnd(t *testing.T) {
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
	for _, test := range []struct {
		name     string
		language judge.Language
		source   string
	}{
		{
			name:     "cpp",
			language: judge.LanguageCPP,
			source: `#include <iostream>
int main() { long long a, b; std::cin >> a >> b; std::cout << a + b << '\n'; }
`,
		},
		{
			name:     "java",
			language: judge.LanguageJava,
			source: `public class Main {
    public static void main(String[] args) { System.out.println("java-ok"); }
}
`,
		},
		{
			name:     "python3",
			language: judge.LanguagePython3,
			source:   "print(\"python-ok\")\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected := "3\n"
			input := "1 2\n"
			if test.language == judge.LanguageJava {
				expected = "java-ok\n"
				input = ""
			}
			if test.language == judge.LanguagePython3 {
				expected = "python-ok\n"
				input = ""
			}
			engine := judge.NewEngine(runner, newFixtureRepository(t, input, expected), nil, nil)
			task := basicTask()
			task.Language = test.language
			task.Source = test.source
			task.OutputDir = t.TempDir()
			response, err := engine.Judge(context.Background(), task)
			if err != nil {
				t.Fatal(err)
			}
			if response.Verdict != judge.VerdictAccepted || response.Compile.Verdict != judge.VerdictAccepted || len(response.Cases) != 1 || response.Cases[0].Verdict != judge.VerdictAccepted {
				t.Fatalf("response = %#v", response)
			}
			if response.Cases[0].OutputPath != "" {
				t.Fatalf("accepted output was retained: %#v", response.Cases[0])
			}
			if _, err := os.Stat(filepath.Join(task.OutputDir, "case-1.out")); !os.IsNotExist(err) {
				t.Fatalf("accepted output file remains: %v", err)
			}
		})
	}
}

func writableCgroupRoot(t *testing.T) string {
	t.Helper()
	root, err := sandbox.CurrentCgroupRoot()
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(root, "justcpc-judge-test-probe")
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
