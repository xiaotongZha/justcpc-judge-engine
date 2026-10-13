package judge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FileSystemRepository is the development and local-deployment repository.
// At startup it indexes only names and absolute file paths. It does not read
// inputs or answers until the engine executes the corresponding test point.
type FileSystemRepository struct {
	problems map[string]Problem
}

type FileSystemRepositoryConfig struct {
	Root         string
	ProblemLimit map[string]map[Language]ResourceLimit
	DefaultLimit map[Language]ResourceLimit
	CompileLimit ResourceLimit
	Compare      CompareOption
}

func NewFileSystemRepository(config FileSystemRepositoryConfig) (*FileSystemRepository, error) {
	if config.Root == "" {
		return nil, fmt.Errorf("problem repository root is required")
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve problem repository root: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read problem repository root: %w", err)
	}
	repository := &FileSystemRepository{problems: make(map[string]Problem)}
	for _, contest := range entries {
		if !contest.IsDir() || !strings.HasPrefix(contest.Name(), "contest-") {
			continue
		}
		year := strings.TrimPrefix(contest.Name(), "contest-")
		casesRoot := filepath.Join(root, contest.Name(), "cases")
		problems, err := os.ReadDir(casesRoot)
		if err != nil {
			return nil, fmt.Errorf("read cases for contest %s: %w", year, err)
		}
		for _, entry := range problems {
			if !entry.IsDir() {
				continue
			}
			id := year + "/" + entry.Name()
			cases, err := indexCases(filepath.Join(casesRoot, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("index problem %s: %w", id, err)
			}
			limits := config.DefaultLimit
			if override, ok := config.ProblemLimit[id]; ok {
				limits = override
			}
			repository.problems[id] = Problem{
				ID:           id,
				Limits:       copyLimits(limits),
				CompileLimit: config.CompileLimit,
				Compare:      config.Compare,
				Cases:        cases,
			}
		}
	}
	if len(repository.problems) == 0 {
		return nil, fmt.Errorf("no problems found under %s", root)
	}
	return repository, nil
}

func (r *FileSystemRepository) Load(_ context.Context, problemID string) (Problem, error) {
	problem, ok := r.problems[problemID]
	if !ok {
		return Problem{}, fmt.Errorf("problem not found")
	}
	problem.Limits = copyLimits(problem.Limits)
	problem.Cases = append([]TestCase(nil), problem.Cases...)
	return problem, nil
}

func indexCases(dir string) ([]TestCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	cases := make([]TestCase, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".in" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".in")
		inputPath := filepath.Join(dir, entry.Name())
		expectedPath := filepath.Join(dir, id+".out")
		if info, err := os.Stat(expectedPath); err != nil || !info.Mode().IsRegular() {
			if err != nil {
				return nil, fmt.Errorf("expected output for case %s: %w", id, err)
			}
			return nil, fmt.Errorf("expected output for case %s is not a regular file", id)
		}
		cases = append(cases, TestCase{ID: id, InputPath: inputPath, ExpectedPath: expectedPath})
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("no test cases")
	}
	sort.Slice(cases, func(i, j int) bool { return caseOrder(cases[i].ID) < caseOrder(cases[j].ID) })
	return cases, nil
}

func copyLimits(source map[Language]ResourceLimit) map[Language]ResourceLimit {
	if source == nil {
		return nil
	}
	result := make(map[Language]ResourceLimit, len(source))
	for language, limit := range source {
		result[language] = limit
	}
	return result
}

func caseOrder(value string) int {
	order, err := strconv.Atoi(value)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return order
}
