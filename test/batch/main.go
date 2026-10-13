// Command batch runs normalized historical submissions through the judge queue.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"justcpc-judge-engine/src/judge"
	"justcpc-judge-engine/src/sandbox"
)

type submission struct {
	Contest  string
	UserID   string
	Problem  string
	Language judge.Language
	Path     string
}

type submissionTask struct {
	Submission submission
	Cases      []judge.TestCase
}

type caseSummary struct {
	ID         string        `json:"id"`
	Verdict    judge.Verdict `json:"verdict"`
	TimeMS     int64         `json:"time_ms"`
	WallTimeMS int64         `json:"wall_time_ms"`
	Memory     uint64        `json:"memory_bytes"`
}

type submissionSummary struct {
	Contest        string         `json:"contest"`
	UserID         string         `json:"user_id"`
	Problem        string         `json:"problem"`
	Language       judge.Language `json:"language"`
	SourcePath     string         `json:"source_path"`
	Verdict        judge.Verdict  `json:"verdict"`
	CompileVerdict judge.Verdict  `json:"compile_verdict"`
	AcceptedCases  int            `json:"accepted_cases"`
	TotalCases     int            `json:"total_cases"`
	FirstFailure   *caseSummary   `json:"first_failure,omitempty"`
	PeakMemory     uint64         `json:"peak_memory_bytes"`
	TotalTimeMS    int64          `json:"total_time_ms"`
	Cases          []caseSummary  `json:"cases"`
}

type batchReport struct {
	GeneratedAt string              `json:"generated_at"`
	Workers     int                 `json:"workers"`
	Contests    []string            `json:"contests"`
	Results     []submissionSummary `json:"results"`
}

func main() {
	rootFS := flag.String("rootfs", "runtime/judge-rootfs", "judge RootFS path")
	testdata := flag.String("testdata", "testdata", "normalized contest data root")
	workRoot := flag.String("work-root", "", "temporary sandbox work directory; default creates and removes one")
	cgroupRoot := flag.String("cgroup-root", "", "delegated cgroup v2 directory")
	contestsFlag := flag.String("contests", "17,18", "comma-separated contest years")
	usersFlag := flag.String("users", "", "optional comma-separated user IDs")
	problemsFlag := flag.String("problems", "", "optional comma-separated problem IDs")
	workers := flag.Int("workers", 10, "maximum simultaneous submissions")
	output := flag.String("output", "test/results/historical-submissions.json", "JSON report path")
	flag.Parse()

	if *workers < 1 || *workers > 10 {
		log.Fatal("workers must be between 1 and 10")
	}
	contests, err := parseContests(*contestsFlag)
	if err != nil {
		log.Fatal(err)
	}
	users := parseFilter(*usersFlag)
	problems := parseFilter(*problemsFlag)
	sandboxWorkRoot := *workRoot
	if sandboxWorkRoot == "" {
		sandboxWorkRoot, err = os.MkdirTemp("", "justcpc-historical-batch-")
		if err != nil {
			log.Fatalf("create temporary work directory: %v", err)
		}
		defer os.RemoveAll(sandboxWorkRoot)
	}
	if *cgroupRoot == "" {
		*cgroupRoot, err = sandbox.CurrentCgroupRoot()
		if err != nil {
			log.Fatalf("discover cgroup root: %v", err)
		}
	}

	runner, err := sandbox.New(sandbox.Config{
		RootFS:     *rootFS,
		WorkRoot:   sandboxWorkRoot,
		CgroupRoot: *cgroupRoot,
	})
	if err != nil {
		log.Fatalf("configure sandbox: %v", err)
	}
	repository, err := judge.NewFileSystemRepository(judge.FileSystemRepositoryConfig{
		Root:         *testdata,
		DefaultLimit: historicalLimits("17"),
		ProblemLimit: historicalProblemLimits(),
	})
	if err != nil {
		log.Fatalf("configure problem repository: %v", err)
	}
	engine := judge.NewEngine(runner, repository, nil, nil)

	cases, err := loadAllCases(*testdata, contests)
	if err != nil {
		log.Fatal(err)
	}
	submissions, err := discoverSubmissions(*testdata, contests, cases, users, problems)
	if err != nil {
		log.Fatal(err)
	}
	tasks, err := splitSubmissions(submissions, cases, 0)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("judging %d submissions with %d workers", len(submissions), *workers)
	results, err := judgeAll(engine, tasks, *workers)
	if err != nil {
		log.Fatal(err)
	}
	report := batchReport{
		GeneratedAt: beijingNow(),
		Workers:     *workers,
		Contests:    contests,
		Results:     results,
	}
	if err := writeReport(*output, report); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d results to %s", len(results), *output)
}

var beijingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func beijingNow() string {
	return time.Now().In(beijingLocation).Format("2006-01-02 15:04:05")
}

func parseContests(value string) ([]string, error) {
	seen := make(map[string]bool)
	var contests []string
	for _, part := range strings.Split(value, ",") {
		year := strings.TrimSpace(part)
		if year != "17" && year != "18" {
			return nil, fmt.Errorf("unsupported contest %q; supported values are 17 and 18", year)
		}
		if !seen[year] {
			seen[year] = true
			contests = append(contests, year)
		}
	}
	if len(contests) == 0 {
		return nil, errors.New("at least one contest is required")
	}
	sort.Strings(contests)
	return contests, nil
}

func parseFilter(value string) map[string]bool {
	filter := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		if value := strings.TrimSpace(part); value != "" {
			filter[value] = true
		}
	}
	return filter
}

func loadAllCases(root string, contests []string) (map[string][]judge.TestCase, error) {
	all := make(map[string][]judge.TestCase)
	for _, contest := range contests {
		problemRoot := filepath.Join(root, "contest-"+contest, "cases")
		problems, err := os.ReadDir(problemRoot)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", problemRoot, err)
		}
		for _, problem := range problems {
			if !problem.IsDir() {
				continue
			}
			cases, err := loadCases(filepath.Join(problemRoot, problem.Name()))
			if err != nil {
				return nil, err
			}
			all[caseKey(contest, problem.Name())] = cases
		}
	}
	return all, nil
}

func loadCases(dir string) ([]judge.TestCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read cases %s: %w", dir, err)
	}
	var cases []judge.TestCase
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".in" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".in")
		expectedPath := filepath.Join(dir, id+".out")
		if info, err := os.Stat(expectedPath); err != nil || !info.Mode().IsRegular() {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("expected output %s is not a regular file", expectedPath)
		}
		cases = append(cases, judge.TestCase{ID: id, InputPath: filepath.Join(dir, entry.Name()), ExpectedPath: expectedPath})
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("no cases in %s", dir)
	}
	sort.Slice(cases, func(i, j int) bool { return numericID(cases[i].ID) < numericID(cases[j].ID) })
	return cases, nil
}

func discoverSubmissions(root string, contests []string, cases map[string][]judge.TestCase, userFilter, problemFilter map[string]bool) ([]submission, error) {
	var result []submission
	for _, contest := range contests {
		submissionRoot := filepath.Join(root, "contest-"+contest, "submissions")
		userDirs, err := os.ReadDir(submissionRoot)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", submissionRoot, err)
		}
		for _, user := range userDirs {
			if !user.IsDir() {
				continue
			}
			if len(userFilter) > 0 && !userFilter[user.Name()] {
				continue
			}
			files, err := os.ReadDir(filepath.Join(submissionRoot, user.Name()))
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.IsDir() {
					continue
				}
				language, ok := languageFor(file.Name())
				if !ok {
					continue
				}
				problem := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
				if len(problemFilter) > 0 && !problemFilter[problem] {
					continue
				}
				if _, ok := cases[caseKey(contest, problem)]; !ok {
					log.Printf("skip %s/%s/%s: no normalized cases", contest, user.Name(), file.Name())
					continue
				}
				result = append(result, submission{
					Contest:  contest,
					UserID:   user.Name(),
					Problem:  problem,
					Language: language,
					Path:     filepath.Join(submissionRoot, user.Name(), file.Name()),
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		return strings.Join([]string{left.Contest, left.UserID, left.Problem, string(left.Language)}, "\x00") <
			strings.Join([]string{right.Contest, right.UserID, right.Problem, string(right.Language)}, "\x00")
	})
	return result, nil
}

func splitSubmissions(submissions []submission, cases map[string][]judge.TestCase, _ uint64) ([]submissionTask, error) {
	var tasks []submissionTask
	for _, submission := range submissions {
		problemCases := cases[caseKey(submission.Contest, submission.Problem)]
		tasks = append(tasks, submissionTask{Submission: submission, Cases: problemCases})
	}
	return tasks, nil
}

func languageFor(name string) (judge.Language, bool) {
	switch filepath.Ext(name) {
	case ".cpp":
		return judge.LanguageCPP, true
	case ".java":
		return judge.LanguageJava, true
	case ".py":
		return judge.LanguagePython3, true
	default:
		return "", false
	}
}

type completedTask struct {
	task     submissionTask
	response judge.Response
	err      error
}

func judgeAll(service judge.Service, tasks []submissionTask, workers int) ([]submissionSummary, error) {
	jobs := make(chan submissionTask)
	completed := make(chan completedTask, workers)
	var workersGroup sync.WaitGroup
	for range workers {
		workersGroup.Add(1)
		go func() {
			defer workersGroup.Done()
			for task := range jobs {
				source, err := os.ReadFile(task.Submission.Path)
				if err != nil {
					completed <- completedTask{task: task, err: fmt.Errorf("read %s: %w", task.Submission.Path, err)}
					continue
				}
				response, err := service.Judge(context.Background(), judge.JudgeTask{
					ProblemID: caseKey(task.Submission.Contest, task.Submission.Problem),
					Language:  task.Submission.Language,
					Source:    string(source),
					Submitter: task.Submission.UserID,
				})
				completed <- completedTask{task: task, response: response, err: err}
			}
		}()
	}
	go func() {
		for _, task := range tasks {
			jobs <- task
		}
		close(jobs)
		workersGroup.Wait()
		close(completed)
	}()

	partial := make([]submissionSummary, 0, len(tasks))
	var firstErr error
	for completedTask := range completed {
		if completedTask.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("judge %s: %w", completedTask.task.Submission.Path, completedTask.err)
			}
			continue
		}
		partial = append(partial, summarize(completedTask.task, completedTask.response))
	}
	if firstErr != nil {
		return nil, firstErr
	}
	results := mergeSummaries(partial)
	sort.Slice(results, func(i, j int) bool {
		left, right := results[i], results[j]
		return strings.Join([]string{left.Contest, left.UserID, left.Problem, string(left.Language)}, "\x00") <
			strings.Join([]string{right.Contest, right.UserID, right.Problem, string(right.Language)}, "\x00")
	})
	return results, nil
}

func limitsFor(contest string, language judge.Language) judge.ResourceLimit {
	if contest == "18" {
		switch language {
		case judge.LanguageCPP:
			return judge.ResourceLimit{TimeLimitMS: 1000, MemoryLimitMB: 256}
		case judge.LanguageJava:
			return judge.ResourceLimit{TimeLimitMS: 1500, MemoryLimitMB: 256}
		case judge.LanguagePython3:
			return judge.ResourceLimit{TimeLimitMS: 2000, MemoryLimitMB: 512}
		}
	}
	return judge.ResourceLimit{TimeLimitMS: 2000, MemoryLimitMB: 512}
}

func historicalLimits(contest string) map[judge.Language]judge.ResourceLimit {
	return map[judge.Language]judge.ResourceLimit{
		judge.LanguageCPP:     limitsFor(contest, judge.LanguageCPP),
		judge.LanguageJava:    limitsFor(contest, judge.LanguageJava),
		judge.LanguagePython3: limitsFor(contest, judge.LanguagePython3),
	}
}

func historicalProblemLimits() map[string]map[judge.Language]judge.ResourceLimit {
	limits := make(map[string]map[judge.Language]judge.ResourceLimit)
	for _, problem := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		limits["18/"+problem] = historicalLimits("18")
	}
	return limits
}

func summarize(task submissionTask, response judge.Response) submissionSummary {
	submission := task.Submission
	result := submissionSummary{
		Contest:        submission.Contest,
		UserID:         submission.UserID,
		Problem:        submission.Problem,
		Language:       submission.Language,
		SourcePath:     submission.Path,
		Verdict:        response.Verdict,
		CompileVerdict: response.Compile.Verdict,
		TotalCases:     len(task.Cases),
		Cases:          make([]caseSummary, 0, len(response.Cases)),
	}
	if response.Compile.Verdict != judge.VerdictAccepted {
		result.FirstFailure = &caseSummary{ID: "compile", Verdict: response.Compile.Verdict}
	}
	for _, test := range response.Cases {
		caseResult := caseSummary{
			ID:         test.ID,
			Verdict:    test.Verdict,
			TimeMS:     test.TimeMS,
			WallTimeMS: test.WallTimeMS,
			Memory:     test.Memory,
		}
		result.Cases = append(result.Cases, caseResult)
		result.TotalTimeMS += test.TimeMS
		if test.Memory > result.PeakMemory {
			result.PeakMemory = test.Memory
		}
		if test.Verdict == judge.VerdictAccepted {
			result.AcceptedCases++
		} else if result.FirstFailure == nil {
			failure := caseResult
			result.FirstFailure = &failure
		}
	}
	return result
}

func mergeSummaries(partial []submissionSummary) []submissionSummary {
	merged := make(map[string]*submissionSummary)
	for _, result := range partial {
		key := submissionSummaryKey(result)
		combined, exists := merged[key]
		if !exists {
			combined = &submissionSummary{
				Contest:        result.Contest,
				UserID:         result.UserID,
				Problem:        result.Problem,
				Language:       result.Language,
				SourcePath:     result.SourcePath,
				Verdict:        judge.VerdictAccepted,
				CompileVerdict: judge.VerdictAccepted,
			}
			merged[key] = combined
		}
		combined.TotalCases += result.TotalCases
		combined.TotalTimeMS += result.TotalTimeMS
		if result.PeakMemory > combined.PeakMemory {
			combined.PeakMemory = result.PeakMemory
		}
		combined.Cases = append(combined.Cases, result.Cases...)
		if result.CompileVerdict != judge.VerdictAccepted && combined.CompileVerdict == judge.VerdictAccepted {
			combined.CompileVerdict = result.CompileVerdict
		}
	}

	results := make([]submissionSummary, 0, len(merged))
	for _, combined := range merged {
		sort.Slice(combined.Cases, func(i, j int) bool {
			return numericID(combined.Cases[i].ID) < numericID(combined.Cases[j].ID)
		})
		if combined.CompileVerdict != judge.VerdictAccepted {
			combined.Verdict = combined.CompileVerdict
			combined.FirstFailure = &caseSummary{ID: "compile", Verdict: combined.CompileVerdict}
		} else {
			for _, test := range combined.Cases {
				if test.Verdict == judge.VerdictAccepted {
					combined.AcceptedCases++
					continue
				}
				if combined.FirstFailure == nil {
					failure := test
					combined.FirstFailure = &failure
					combined.Verdict = test.Verdict
				}
			}
		}
		results = append(results, *combined)
	}
	return results
}

func submissionSummaryKey(result submissionSummary) string {
	return strings.Join([]string{result.Contest, result.UserID, result.Problem, string(result.Language)}, "\x00")
}

func writeReport(path string, report batchReport) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return err
	}
	file, err := os.Create(absolute)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func caseKey(contest, problem string) string {
	return contest + "/" + problem
}

func numericID(value string) int {
	var result int
	for _, char := range value {
		if char < '0' || char > '9' {
			return result
		}
		result = result*10 + int(char-'0')
	}
	return result
}
