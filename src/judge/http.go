package judge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"justcpc-judge-engine/src/capacity"
)

const maxJudgeRequestBytes int64 = 1 << 30

// NewHTTPHandler exposes synchronous execution only. Durable queuing and
// retries belong to the calling business service, not the judge engine.
func NewHTTPHandler(service Service, maxConcurrent int) (http.Handler, error) {
	return NewHTTPHandlerWithCapacity(service, capacity.Selection{Workers: maxConcurrent, Mode: "manual"})
}

// NewHTTPHandlerWithCapacity publishes startup capacity and applies admission
// limits before compilation. The caller retains all durable queue ownership.
func NewHTTPHandlerWithCapacity(service Service, selection capacity.Selection) (http.Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("judge service is required")
	}
	if selection.Workers <= 0 {
		return nil, fmt.Errorf("maximum concurrent executions must be positive")
	}
	gate := executionGate{selection: selection}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/capacity", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method must be GET"})
			return
		}
		gate.mu.Lock()
		status := struct {
			capacity.Selection
			Active           int    `json:"active"`
			ReservedMemoryMB uint64 `json:"reserved_memory_mb"`
		}{selection, gate.active, gate.reservedMB}
		gate.mu.Unlock()
		writeJSON(writer, http.StatusOK, status)
	})
	mux.HandleFunc("/api/execute", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method must be POST"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxJudgeRequestBytes)
		defer request.Body.Close()
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var input JudgeTask
		if err := decoder.Decode(&input); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if err := ensureSingleJSONValue(decoder); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		memoryMB := requestMemoryMB(input)
		if !gate.acquire(memoryMB) {
			writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "judge engine is at execution capacity"})
			return
		}
		defer gate.release(memoryMB)
		response, err := service.Judge(request.Context(), input)
		if err != nil {
			response = Response{
				Verdict: VerdictSystemError,
				Compile: CompileResult{Verdict: VerdictSystemError, Error: err.Error()},
			}
		}
		writeJSON(writer, http.StatusOK, response)
	})
	return mux, nil
}

type executionGate struct {
	mu         sync.Mutex
	selection  capacity.Selection
	active     int
	reservedMB uint64
}

func (g *executionGate) acquire(memoryMB uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= g.selection.Workers {
		return false
	}
	if g.selection.Mode == "auto" && g.active > 0 {
		budget := g.selection.MemoryBudgetMB
		if g.reservedMB >= budget || memoryMB > budget-g.reservedMB {
			return false
		}
	}
	// Allow a single large-limit task to make progress; a configured limit is
	// not its actual allocation. Other tasks wait until its reservation ends.
	g.active++
	g.reservedMB += memoryMB
	return true
}

func (g *executionGate) release(memoryMB uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
	g.reservedMB -= memoryMB
}

func requestMemoryMB(task JudgeTask) uint64 {
	compileMB, runMB := uint64(512), uint64(256)
	if task.Problem != nil {
		if task.Problem.CompileLimit.MemoryLimitMB > 0 {
			compileMB = task.Problem.CompileLimit.MemoryLimitMB
		}
		if limit := task.Problem.Limits[task.Language]; limit.MemoryLimitMB > 0 {
			runMB = limit.MemoryLimitMB
		}
	}
	// Compile and run sequentially. Reserve their larger limit plus service
	// overhead, at least the 1 GiB used by the startup slot estimate.
	return max(1024, max(compileMB, runMB)+128)
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body contains multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
