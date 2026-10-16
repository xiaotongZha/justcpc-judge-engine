package judge

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"justcpc-judge-engine/src/sandbox"
)

type Engine struct {
	runner     Runner
	compiler   Compiler
	comparator Comparator
}

func NewEngine(runner Runner, compiler Compiler, comparator Comparator) *Engine {
	if compiler == nil {
		compiler = NewCompiler(runner)
	}
	if comparator == nil {
		comparator = NewComparator()
	}
	return &Engine{runner: runner, compiler: compiler, comparator: comparator}
}

func (e *Engine) Judge(ctx context.Context, task JudgeTask) (Response, error) {
	if err := validateTask(task); err != nil {
		return Response{}, err
	}
	problem := *task.Problem
	if problem.ID == "" {
		problem.ID = task.ProblemID
	}
	if problem.ID != task.ProblemID {
		return Response{}, fmt.Errorf("task problem ID %q does not match metadata ID %q", task.ProblemID, problem.ID)
	}
	if err := validateProblem(problem, task.Language); err != nil {
		return Response{}, err
	}
	if task.OutputDir != "" {
		if err := os.MkdirAll(task.OutputDir, 0700); err != nil {
			return Response{}, fmt.Errorf("create output directory: %w", err)
		}
	}
	runLimit := problem.Limits[task.Language].sandboxLimit(defaultRunLimit())
	compileLimit := problem.CompileLimit.sandboxLimit(defaultCompileLimit())
	program, compilation := e.compiler.Compile(ctx, Submission{Language: task.Language, Source: task.Source}, compileLimit)
	response := Response{Compile: compilation}
	if compilation.Verdict != VerdictAccepted {
		response.Verdict = compilation.Verdict
		return response, nil
	}

	response.Verdict = VerdictAccepted
	for _, test := range problem.Cases {
		stagingPath := ""
		outputPath := ""
		if task.OutputDir != "" {
			outputPath = filepath.Join(task.OutputDir, test.ID+".out")
			stagingPath = filepath.Join(task.OutputDir, ".staging", test.ID+".out")
			if err := os.MkdirAll(filepath.Dir(stagingPath), 0700); err != nil {
				return Response{}, fmt.Errorf("create output staging directory: %w", err)
			}
			if err := os.Remove(stagingPath); err != nil && !os.IsNotExist(err) {
				return Response{}, fmt.Errorf("remove stale staged output: %w", err)
			}
		}
		result := e.runner.Run(ctx, sandbox.Task{
			Args:       runtimeArgs(task.Language, program.Args, runLimit),
			Files:      program.Files,
			StdinPath:  test.InputPath,
			StdoutPath: stagingPath,
			Limit:      runLimit,
			Profile:    sandbox.ProfileRun,
		})
		caseResult := caseFromSandbox(test.ID, result)
		if result.Status == sandbox.StatusNormal {
			var matched bool
			var err error
			if stagingPath != "" {
				matched, err = e.comparator.ComparePaths(stagingPath, test.ExpectedPath, problem.Compare)
			} else {
				matched, err = e.comparator.CompareFile(result.Stdout, test.ExpectedPath, problem.Compare)
			}
			if err != nil {
				caseResult.Verdict = VerdictSystemError
				caseResult.Error = err.Error()
			} else if matched {
				caseResult.Verdict = VerdictAccepted
			} else {
				caseResult.Verdict = VerdictWrongAnswer
				if stagingPath != "" {
					if err := os.Rename(stagingPath, outputPath); err != nil {
						caseResult.Verdict = VerdictSystemError
						caseResult.Error = fmt.Sprintf("store wrong-answer output: %v", err)
					} else {
						caseResult.OutputPath = outputPath
					}
				}
			}
		}
		if stagingPath != "" {
			if err := os.Remove(stagingPath); err != nil && !os.IsNotExist(err) && caseResult.Verdict != VerdictSystemError {
				caseResult.Verdict = VerdictSystemError
				caseResult.Error = fmt.Sprintf("remove staged output: %v", err)
			}
		}
		response.Cases = append(response.Cases, caseResult)
		if response.Verdict == VerdictAccepted && caseResult.Verdict != VerdictAccepted {
			response.Verdict = caseResult.Verdict
		}
		if problem.StopOnFirstFailure && caseResult.Verdict != VerdictAccepted {
			break
		}
	}
	return response, nil
}

func runtimeArgs(language Language, args []string, limit sandbox.Limit) []string {
	if language != LanguageJava || len(args) == 0 {
		return args
	}
	heapMiB := limit.Memory >> 21
	if heapMiB < 16 {
		heapMiB = 16
	}
	javaArgs := []string{args[0], "-Xms16m", fmt.Sprintf("-Xmx%dm", heapMiB)}
	return append(javaArgs, args[1:]...)
}

func caseFromSandbox(id string, result sandbox.Result) CaseResult {
	caseResult := CaseResult{
		ID:                   id,
		SandboxStatus:        string(result.Status),
		TimeMS:               result.CPUTime.Milliseconds(),
		WallTimeMS:           result.WallTime.Milliseconds(),
		Memory:               result.Memory,
		ProcessPeak:          result.ProcessPeak,
		MemoryEstimated:      result.MemoryEstimated,
		ProcessPeakEstimated: result.ProcessPeakEstimated,
		Stderr:               truncateDiagnostic(result.Stderr),
		Error:                truncateDiagnostic([]byte(result.Error)),
	}
	switch result.Status {
	case sandbox.StatusNormal:
		caseResult.Verdict = VerdictAccepted
	case sandbox.StatusTimeLimitExceeded:
		caseResult.Verdict = VerdictTimeLimitExceeded
	case sandbox.StatusMemoryLimitExceeded:
		caseResult.Verdict = VerdictMemoryLimitExceeded
	case sandbox.StatusSystemError:
		caseResult.Verdict = VerdictSystemError
	default:
		caseResult.Verdict = VerdictRuntimeError
	}
	return caseResult
}

const maxDiagnosticBytes = 64 << 10

func truncateDiagnostic(value []byte) string {
	if len(value) <= maxDiagnosticBytes {
		return string(value)
	}
	return string(value[:maxDiagnosticBytes]) + "\n[diagnostic truncated]\n"
}

func validateTask(task JudgeTask) error {
	if task.ProblemID == "" {
		return fmt.Errorf("problem_id is required")
	}
	if task.Problem == nil {
		return fmt.Errorf("problem is required")
	}
	if task.Language != LanguageCPP && task.Language != LanguageJava && task.Language != LanguagePython3 {
		return fmt.Errorf("language must be cpp, java, or python3")
	}
	if task.OutputDir != "" && !filepath.IsAbs(task.OutputDir) {
		return fmt.Errorf("output_dir must be absolute")
	}
	return nil
}

func validateProblem(problem Problem, language Language) error {
	if len(problem.Cases) == 0 {
		return fmt.Errorf("at least one test case is required")
	}
	if _, ok := problem.Limits[language]; !ok {
		return fmt.Errorf("problem %q has no limits for language %q", problem.ID, language)
	}
	seen := make(map[string]struct{}, len(problem.Cases))
	for _, test := range problem.Cases {
		if test.ID == "" {
			return fmt.Errorf("test case id is required")
		}
		if filepath.Base(test.ID) != test.ID || test.ID == "." {
			return fmt.Errorf("test case id %q must not contain a path separator", test.ID)
		}
		if _, exists := seen[test.ID]; exists {
			return fmt.Errorf("duplicate test case id %q", test.ID)
		}
		seen[test.ID] = struct{}{}
		for label, path := range map[string]string{"input": test.InputPath, "expected": test.ExpectedPath} {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("test case %q %s file: %w", test.ID, label, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("test case %q %s path is not a regular file", test.ID, label)
			}
		}
	}
	if problem.Compare.FloatingPoint && (problem.Compare.Epsilon < 0 || math.IsNaN(problem.Compare.Epsilon) || math.IsInf(problem.Compare.Epsilon, 0)) {
		return fmt.Errorf("epsilon must be a finite non-negative number")
	}
	return nil
}

func defaultRunLimit() sandbox.Limit {
	return sandbox.Limit{
		Time:       time.Second,
		WallTime:   2 * time.Second,
		Memory:     256 << 20,
		Processes:  64,
		OutputSize: 16 << 20,
		Stack:      64 << 20,
	}
}

func defaultCompileLimit() sandbox.Limit {
	return sandbox.Limit{
		Time:         10 * time.Second,
		WallTime:     20 * time.Second,
		Memory:       512 << 20,
		Processes:    128,
		OutputSize:   16 << 20,
		ArtifactSize: 64 << 20,
		Stack:        64 << 20,
	}
}

func (l ResourceLimit) sandboxLimit(defaults sandbox.Limit) sandbox.Limit {
	limit := defaults
	if l.TimeLimitMS > 0 {
		limit.Time = time.Duration(l.TimeLimitMS) * time.Millisecond
	}
	if l.WallTimeLimitMS > 0 {
		limit.WallTime = time.Duration(l.WallTimeLimitMS) * time.Millisecond
	} else if l.TimeLimitMS > 0 {
		limit.WallTime = limit.Time * 2
	}
	if l.MemoryLimitMB > 0 {
		limit.Memory = l.MemoryLimitMB << 20
	}
	if l.ProcessLimit > 0 {
		limit.Processes = l.ProcessLimit
	}
	if l.OutputLimitKB > 0 {
		limit.OutputSize = l.OutputLimitKB << 10
	}
	if l.StackLimitMB > 0 {
		limit.Stack = l.StackLimitMB << 20
	}
	return limit
}
