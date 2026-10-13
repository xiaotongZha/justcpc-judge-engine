package judge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxJudgeRequestBytes int64 = 1 << 30

// NewHTTPHandler exposes synchronous execution only. Durable queuing and
// retries belong to the calling business service, not the judge engine.
func NewHTTPHandler(service Service, maxConcurrent int) (http.Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("judge service is required")
	}
	if maxConcurrent <= 0 {
		return nil, fmt.Errorf("maximum concurrent executions must be positive")
	}
	slots := make(chan struct{}, maxConcurrent)
	mux := http.NewServeMux()
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
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "judge engine is at execution capacity"})
			return
		}
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
