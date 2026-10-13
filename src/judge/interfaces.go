package judge

import (
	"context"

	"justcpc-judge-engine/src/sandbox"
)

// Runner is the only execution dependency of the judge layer.
type Runner interface {
	Run(context.Context, sandbox.Task) sandbox.Result
}

// Compiler turns one submitted source file into a runnable program.
type Compiler interface {
	Compile(context.Context, Submission, sandbox.Limit) (Program, CompileResult)
}

// Comparator decides whether a normal program output matches an answer.
type Comparator interface {
	Compare(actual, expected []byte, option CompareOption) (bool, error)
	CompareFile(actual []byte, expectedPath string, option CompareOption) (bool, error)
	ComparePaths(actualPath, expectedPath string, option CompareOption) (bool, error)
}

// Service is the business interface used by the HTTP layer and future callers.
type Service interface {
	Judge(context.Context, JudgeTask) (Response, error)
}

// ProblemRepository isolates judging from the deployed problem store.
// Implementations return paths to test data rather than loading all cases.
type ProblemRepository interface {
	Load(context.Context, string) (Problem, error)
}

type Submission struct {
	Language Language
	Source   string
}

// Program contains only data that can be copied into a fresh sandbox task.
type Program struct {
	Args  []string
	Files []sandbox.InputFile
}
