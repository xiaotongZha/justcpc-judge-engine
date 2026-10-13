package judge_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"justcpc-judge-engine/src/judge"
)

type fixtureRepository struct {
	problem judge.Problem
}

func (r fixtureRepository) Load(_ context.Context, id string) (judge.Problem, error) {
	if id != r.problem.ID {
		return judge.Problem{}, os.ErrNotExist
	}
	return r.problem, nil
}

func newFixtureRepository(t *testing.T, input, expected string) judge.ProblemRepository {
	t.Helper()
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "1.in")
	expectedPath := filepath.Join(dir, "1.out")
	if err := os.WriteFile(inputPath, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expectedPath, []byte(expected), 0600); err != nil {
		t.Fatal(err)
	}
	limits := map[judge.Language]judge.ResourceLimit{
		judge.LanguageCPP:     {TimeLimitMS: 1000, MemoryLimitMB: 256},
		judge.LanguageJava:    {TimeLimitMS: 1000, MemoryLimitMB: 256},
		judge.LanguagePython3: {TimeLimitMS: 1000, MemoryLimitMB: 256},
	}
	return fixtureRepository{problem: judge.Problem{
		ID:     "test/problem",
		Limits: limits,
		Cases:  []judge.TestCase{{ID: "case-1", InputPath: inputPath, ExpectedPath: expectedPath}},
	}}
}

func basicTask() judge.JudgeTask {
	return judge.JudgeTask{
		ProblemID: "test/problem",
		Language:  judge.LanguageCPP,
		Source:    "int main() {}",
	}
}
