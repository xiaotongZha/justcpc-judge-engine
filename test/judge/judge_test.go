package judge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"justcpc-judge-engine/src/judge"
	"justcpc-judge-engine/src/sandbox"
)

type scriptedRunner struct {
	results []sandbox.Result
	tasks   []sandbox.Task
	onRun   func(sandbox.Task)
}

func (r *scriptedRunner) Run(_ context.Context, task sandbox.Task) sandbox.Result {
	r.tasks = append(r.tasks, task)
	if r.onRun != nil {
		r.onRun(task)
	}
	if len(r.results) == 0 {
		return sandbox.Result{Status: sandbox.StatusSystemError, Error: "unexpected run"}
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result
}

func TestJudgeClassifiesCompilationAndTestResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []sandbox.Result
		expected judge.Verdict
	}{
		{
			name: "accepted",
			results: []sandbox.Result{
				{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
				{Status: sandbox.StatusNormal, Stdout: []byte("3\n")},
			},
			expected: judge.VerdictAccepted,
		},
		{
			name: "wrong answer",
			results: []sandbox.Result{
				{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
				{Status: sandbox.StatusNormal, Stdout: []byte("4\n")},
			},
			expected: judge.VerdictWrongAnswer,
		},
		{
			name: "time limit",
			results: []sandbox.Result{
				{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
				{Status: sandbox.StatusTimeLimitExceeded},
			},
			expected: judge.VerdictTimeLimitExceeded,
		},
		{
			name: "memory limit",
			results: []sandbox.Result{
				{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
				{Status: sandbox.StatusMemoryLimitExceeded},
			},
			expected: judge.VerdictMemoryLimitExceeded,
		},
		{
			name: "runtime error",
			results: []sandbox.Result{
				{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
				{Status: sandbox.StatusNonzeroExit},
			},
			expected: judge.VerdictRuntimeError,
		},
		{
			name: "system error",
			results: []sandbox.Result{
				{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
				{Status: sandbox.StatusSystemError, Error: "cgroup failed"},
			},
			expected: judge.VerdictSystemError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedRunner{results: test.results}
			engine := judge.NewEngine(runner, newFixtureRepository(t, "1 2\n", "3\n"), nil, nil)
			response, err := engine.Judge(context.Background(), basicTask())
			if err != nil {
				t.Fatal(err)
			}
			if response.Verdict != test.expected {
				t.Fatalf("verdict = %s, want %s", response.Verdict, test.expected)
			}
			if len(response.Cases) != 1 || response.Cases[0].Verdict != test.expected {
				t.Fatalf("case results = %#v", response.Cases)
			}
			if len(runner.tasks) != 2 || runner.tasks[0].Profile != sandbox.ProfileCompile || runner.tasks[1].Profile != sandbox.ProfileRun {
				t.Fatalf("tasks = %#v", runner.tasks)
			}
			if runner.tasks[1].StdinPath == "" || len(runner.tasks[1].Stdin) != 0 {
				t.Fatalf("run task must use the test input path: %#v", runner.tasks[1])
			}
		})
	}
}

func TestJudgeReturnsCEWithoutRunningTests(t *testing.T) {
	runner := &scriptedRunner{results: []sandbox.Result{{Status: sandbox.StatusNonzeroExit, Stderr: []byte("Main.cpp: error")}}}
	engine := judge.NewEngine(runner, newFixtureRepository(t, "1 2\n", "3\n"), nil, nil)
	response, err := engine.Judge(context.Background(), basicTask())
	if err != nil {
		t.Fatal(err)
	}
	if response.Verdict != judge.VerdictCompilationError || response.Compile.Verdict != judge.VerdictCompilationError {
		t.Fatalf("response = %#v", response)
	}
	if len(response.Cases) != 0 || len(runner.tasks) != 1 {
		t.Fatalf("compile failure should not run tests: %#v", response.Cases)
	}
}

func TestComparatorFloatingPoint(t *testing.T) {
	comparator := judge.NewComparator()
	matched, err := comparator.Compare([]byte("1.0000004 label\n"), []byte("1 label\n"), judge.CompareOption{FloatingPoint: true, Epsilon: 1e-6})
	if err != nil || !matched {
		t.Fatalf("matched = %v, err = %v", matched, err)
	}
	matched, err = comparator.Compare([]byte("1.0001\n"), []byte("1\n"), judge.CompareOption{FloatingPoint: true, Epsilon: 1e-6})
	if err != nil || matched {
		t.Fatalf("matched = %v, err = %v", matched, err)
	}
}

func TestJudgeStoresOnlyWrongAnswerOutput(t *testing.T) {
	for _, test := range []struct {
		name        string
		actual      string
		wantVerdict judge.Verdict
		wantStored  bool
	}{
		{name: "accepted output is discarded", actual: "3\n", wantVerdict: judge.VerdictAccepted},
		{name: "wrong answer output is retained", actual: "4\n", wantVerdict: judge.VerdictWrongAnswer, wantStored: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedRunner{
				results: []sandbox.Result{
					{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
					{Status: sandbox.StatusNormal},
				},
				onRun: func(task sandbox.Task) {
					if task.StdoutPath == "" {
						return
					}
					if err := os.MkdirAll(filepath.Dir(task.StdoutPath), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(task.StdoutPath, []byte(test.actual), 0600); err != nil {
						t.Fatal(err)
					}
				},
			}
			outputDir := t.TempDir()
			engine := judge.NewEngine(runner, newFixtureRepository(t, "1 2\n", "3\n"), nil, nil)
			task := basicTask()
			task.OutputDir = outputDir
			response, err := engine.Judge(context.Background(), task)
			if err != nil {
				t.Fatal(err)
			}
			caseResult := response.Cases[0]
			if caseResult.Verdict != test.wantVerdict {
				t.Fatalf("verdict = %s, want %s", caseResult.Verdict, test.wantVerdict)
			}
			finalPath := filepath.Join(outputDir, "case-1.out")
			if test.wantStored {
				if caseResult.OutputPath != finalPath {
					t.Fatalf("output path = %q, want %q", caseResult.OutputPath, finalPath)
				}
				output, err := os.ReadFile(finalPath)
				if err != nil || string(output) != test.actual {
					t.Fatalf("stored output = %q, error = %v", output, err)
				}
			} else if caseResult.OutputPath != "" {
				t.Fatalf("accepted output path = %q", caseResult.OutputPath)
			} else if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
				t.Fatalf("accepted output was retained: %v", err)
			}
			stagingPath := filepath.Join(outputDir, ".staging", "case-1.out")
			if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
				t.Fatalf("staged output remains: %v", err)
			}
		})
	}
}

func TestHTTPJudge(t *testing.T) {
	runner := &scriptedRunner{results: []sandbox.Result{
		{Status: sandbox.StatusNormal, Files: []sandbox.OutputFile{{Path: "Main", Data: []byte("binary"), Mode: 0755}}},
		{Status: sandbox.StatusNormal, Stdout: []byte("3\n")},
	}}
	handler, err := judge.NewHTTPHandler(judge.NewEngine(runner, newFixtureRepository(t, "1 2\n", "3\n"), nil, nil), 1)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(basicTask())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response judge.Response
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Verdict != judge.VerdictAccepted || len(response.Cases) != 1 || response.Cases[0].Verdict != judge.VerdictAccepted {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPJudgeAcceptsRequestAboveEightMiB(t *testing.T) {
	handler, err := judge.NewHTTPHandler(judgeServiceFunc(func(context.Context, judge.JudgeTask) (judge.Response, error) {
		return judge.Response{Verdict: judge.VerdictAccepted}, nil
	}), 1)
	if err != nil {
		t.Fatal(err)
	}

	requestBody := basicTask()
	requestBody.Source = strings.Repeat("x", 9<<20)
	body, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHTTPJudgeRejectsInlineTestData(t *testing.T) {
	handler, err := judge.NewHTTPHandler(judgeServiceFunc(func(context.Context, judge.JudgeTask) (judge.Response, error) {
		return judge.Response{Verdict: judge.VerdictAccepted}, nil
	}), 1)
	if err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"problem_id":"test/problem","language":"cpp","source":"int main(){}","tests":[{"input":"must not be queued"}]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/execute", body)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

type judgeServiceFunc func(context.Context, judge.JudgeTask) (judge.Response, error)

func (f judgeServiceFunc) Judge(ctx context.Context, task judge.JudgeTask) (judge.Response, error) {
	return f(ctx, task)
}

func TestHTTPJudgeRejectsRequestsAboveExecutionCapacity(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	handler, err := judge.NewHTTPHandler(judgeServiceFunc(func(ctx context.Context, _ judge.JudgeTask) (judge.Response, error) {
		started <- struct{}{}
		select {
		case <-release:
			return judge.Response{Verdict: judge.VerdictAccepted}, nil
		case <-ctx.Done():
			return judge.Response{}, ctx.Err()
		}
	}), 1)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(basicTask())
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewReader(body)))
		firstDone <- recorder
	}()
	<-started

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewReader(body)))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", second.Code, second.Body.String())
	}
	close(release)
	first := <-firstDone
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
	}
}
