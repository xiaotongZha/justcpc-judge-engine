// Package judge implements compilation, test execution, and output comparison.
package judge

// Verdict is the externally visible classification of a submission or test.
type Verdict string

const (
	VerdictAccepted            Verdict = "AC"
	VerdictWrongAnswer         Verdict = "WA"
	VerdictTimeLimitExceeded   Verdict = "TLE"
	VerdictMemoryLimitExceeded Verdict = "MLE"
	VerdictCompilationError    Verdict = "CE"
	VerdictRuntimeError        Verdict = "RE"
	VerdictSystemError         Verdict = "SYS"
)

type Language string

const (
	LanguageCPP     Language = "cpp"
	LanguageJava    Language = "java"
	LanguagePython3 Language = "python3"
)

// ResourceLimit uses contest-facing units so JSON callers do not have to send
// Go duration values or raw byte counts.
type ResourceLimit struct {
	TimeLimitMS     int64  `json:"time_limit_ms"`
	WallTimeLimitMS int64  `json:"wall_time_limit_ms"`
	MemoryLimitMB   uint64 `json:"memory_limit_mb"`
	ProcessLimit    uint64 `json:"process_limit"`
	OutputLimitKB   uint64 `json:"output_limit_kb"`
	StackLimitMB    uint64 `json:"stack_limit_mb"`
}

type CompareOption struct {
	FloatingPoint bool    `json:"floating_point"`
	Epsilon       float64 `json:"epsilon"`
}

type TestCase struct {
	ID           string `json:"id"`
	InputPath    string `json:"input_path"`
	ExpectedPath string `json:"expected_path"`
}

// Problem is deployment-owned metadata. Test point data stays on the local
// filesystem; only its locations are retained here.
type Problem struct {
	ID                 string                     `json:"id"`
	Limits             map[Language]ResourceLimit `json:"limits"`
	CompileLimit       ResourceLimit              `json:"compile_limit"`
	Compare            CompareOption              `json:"compare"`
	StopOnFirstFailure bool                       `json:"stop_on_first_failure"`
	Cases              []TestCase                 `json:"cases"`
}

// JudgeTask carries OJ-supplied metadata and test file paths, never test data.
type JudgeTask struct {
	ProblemID string   `json:"problem_id"`
	Language  Language `json:"language"`
	Source    string   `json:"source"`
	// Problem is required and supplies resource limits and test file paths.
	Problem   *Problem `json:"problem"`
	Submitter string   `json:"submitter,omitempty"`
	// OutputDir is an absolute, business-owned directory for this submission's
	// per-test stdout files.
	OutputDir string `json:"output_dir,omitempty"`
}

type CompileResult struct {
	Verdict       Verdict `json:"verdict"`
	SandboxStatus string  `json:"sandbox_status"`
	Stdout        string  `json:"stdout"`
	Stderr        string  `json:"stderr"`
	Error         string  `json:"error"`
}

type CaseResult struct {
	ID            string  `json:"id"`
	Verdict       Verdict `json:"verdict"`
	SandboxStatus string  `json:"sandbox_status"`
	TimeMS        int64   `json:"time_ms"`
	WallTimeMS    int64   `json:"wall_time_ms"`
	Memory        uint64  `json:"memory"`
	ProcessPeak   uint64  `json:"process_peak"`
	OutputPath    string  `json:"output_path,omitempty"`
	Stderr        string  `json:"stderr"`
	Error         string  `json:"error"`
}

type Response struct {
	Verdict Verdict       `json:"verdict"`
	Compile CompileResult `json:"compile"`
	Cases   []CaseResult  `json:"cases"`
}
