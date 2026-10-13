package judge

import (
	"context"

	"justcpc-judge-engine/src/sandbox"
)

type toolchainCompiler struct {
	runner Runner
}

func NewCompiler(runner Runner) Compiler {
	return &toolchainCompiler{runner: runner}
}

func (c *toolchainCompiler) Compile(ctx context.Context, submission Submission, limit sandbox.Limit) (Program, CompileResult) {
	var task sandbox.Task
	var program Program
	switch submission.Language {
	case LanguageCPP:
		task = sandbox.Task{
			Args:         []string{"/usr/bin/g++", "-std=c++17", "-O2", "-pipe", "/w/Main.cpp", "-o", "/w/Main"},
			Files:        []sandbox.InputFile{{Path: "Main.cpp", Data: []byte(submission.Source), Mode: 0644}},
			CollectPaths: []string{"Main"},
			Limit:        limit,
			Profile:      sandbox.ProfileCompile,
		}
		program.Args = []string{"/w/Main"}
	case LanguageJava:
		task = sandbox.Task{
			Args:         []string{"/usr/bin/javac", "-encoding", "UTF-8", "-d", "/w/classes", "/w/Main.java"},
			Files:        []sandbox.InputFile{{Path: "Main.java", Data: []byte(submission.Source), Mode: 0644}},
			CollectPaths: []string{"classes"},
			Limit:        limit,
			Profile:      sandbox.ProfileCompile,
		}
		program.Args = []string{"/usr/bin/java", "-cp", "/w/classes", "Main"}
	case LanguagePython3:
		task = sandbox.Task{
			Args:    []string{"/usr/bin/python3", "-m", "py_compile", "/w/main.py"},
			Files:   []sandbox.InputFile{{Path: "main.py", Data: []byte(submission.Source), Mode: 0644}},
			Limit:   limit,
			Profile: sandbox.ProfileCompile,
		}
		program.Args = []string{"/usr/bin/python3", "/w/main.py"}
		program.Files = []sandbox.InputFile{{Path: "main.py", Data: []byte(submission.Source), Mode: 0644}}
	default:
		return Program{}, CompileResult{Verdict: VerdictSystemError, Error: "unsupported language"}
	}

	result := c.runner.Run(ctx, task)
	compileResult := CompileResult{
		SandboxStatus: string(result.Status),
		Stdout:        truncateDiagnostic(result.Stdout),
		Stderr:        truncateDiagnostic(result.Stderr),
		Error:         truncateDiagnostic([]byte(result.Error)),
	}
	if result.Status == sandbox.StatusNormal {
		if submission.Language != LanguagePython3 {
			program.Files = make([]sandbox.InputFile, 0, len(result.Files))
			for _, file := range result.Files {
				program.Files = append(program.Files, sandbox.InputFile{Path: file.Path, Data: file.Data, Mode: file.Mode})
			}
		}
		compileResult.Verdict = VerdictAccepted
		return program, compileResult
	}
	if result.Status == sandbox.StatusSystemError {
		compileResult.Verdict = VerdictSystemError
	} else {
		compileResult.Verdict = VerdictCompilationError
	}
	return Program{}, compileResult
}
