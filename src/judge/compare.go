package judge

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type outputComparator struct{}

func NewComparator() Comparator {
	return outputComparator{}
}

func (outputComparator) Compare(actual, expected []byte, option CompareOption) (bool, error) {
	if !option.FloatingPoint {
		return normalizeOutput(string(actual)) == normalizeOutput(string(expected)), nil
	}
	if option.Epsilon < 0 || math.IsNaN(option.Epsilon) || math.IsInf(option.Epsilon, 0) {
		return false, fmt.Errorf("epsilon must be a finite non-negative number")
	}
	actualTokens := strings.Fields(string(actual))
	expectedTokens := strings.Fields(string(expected))
	if len(actualTokens) != len(expectedTokens) {
		return false, nil
	}
	for i := range actualTokens {
		actualNumber, actualErr := strconv.ParseFloat(actualTokens[i], 64)
		expectedNumber, expectedErr := strconv.ParseFloat(expectedTokens[i], 64)
		if actualErr != nil || expectedErr != nil {
			if actualTokens[i] != expectedTokens[i] {
				return false, nil
			}
			continue
		}
		if math.IsNaN(actualNumber) || math.IsNaN(expectedNumber) || math.IsInf(actualNumber, 0) || math.IsInf(expectedNumber, 0) {
			return false, nil
		}
		if math.Abs(actualNumber-expectedNumber) > option.Epsilon {
			return false, nil
		}
	}
	return true, nil
}

// CompareFile reads one answer file only after its corresponding program run
// completed. The queue and problem metadata retain only the path.
func (comparator outputComparator) CompareFile(actual []byte, expectedPath string, option CompareOption) (bool, error) {
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		return false, fmt.Errorf("read expected output %q: %w", expectedPath, err)
	}
	return comparator.Compare(actual, expected, option)
}

// ComparePaths compares two output files without loading either entire file
// into the judge process. This is the normal path for stored contestant output.
func (outputComparator) ComparePaths(actualPath, expectedPath string, option CompareOption) (bool, error) {
	actual, err := os.Open(actualPath)
	if err != nil {
		return false, fmt.Errorf("open actual output %q: %w", actualPath, err)
	}
	defer actual.Close()
	expected, err := os.Open(expectedPath)
	if err != nil {
		return false, fmt.Errorf("open expected output %q: %w", expectedPath, err)
	}
	defer expected.Close()
	if option.FloatingPoint {
		return compareFloatReaders(actual, expected, option)
	}
	return compareExactReaders(actual, expected)
}

func compareExactReaders(actual, expected *os.File) (bool, error) {
	left := newNormalizedLineReader(actual)
	right := newNormalizedLineReader(expected)
	for {
		leftLine, leftOK, err := left.Next()
		if err != nil {
			return false, err
		}
		rightLine, rightOK, err := right.Next()
		if err != nil {
			return false, err
		}
		if leftOK != rightOK {
			return false, nil
		}
		if !leftOK {
			return true, nil
		}
		if leftLine != rightLine {
			return false, nil
		}
	}
}

func compareFloatReaders(actual, expected *os.File, option CompareOption) (bool, error) {
	if option.Epsilon < 0 || math.IsNaN(option.Epsilon) || math.IsInf(option.Epsilon, 0) {
		return false, fmt.Errorf("epsilon must be a finite non-negative number")
	}
	left := newTokenReader(actual)
	right := newTokenReader(expected)
	for {
		leftToken, leftOK, err := left.Next()
		if err != nil {
			return false, err
		}
		rightToken, rightOK, err := right.Next()
		if err != nil {
			return false, err
		}
		if leftOK != rightOK {
			return false, nil
		}
		if !leftOK {
			return true, nil
		}
		leftNumber, leftErr := strconv.ParseFloat(leftToken, 64)
		rightNumber, rightErr := strconv.ParseFloat(rightToken, 64)
		if leftErr != nil || rightErr != nil {
			if leftToken != rightToken {
				return false, nil
			}
			continue
		}
		if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) || math.IsInf(leftNumber, 0) || math.IsInf(rightNumber, 0) || math.Abs(leftNumber-rightNumber) > option.Epsilon {
			return false, nil
		}
	}
}

type normalizedLineReader struct {
	reader         *bufio.Reader
	pendingBlanks  int
	emittingBlanks int
	readyLine      string
	hasReadyLine   bool
	done           bool
}

func newNormalizedLineReader(file *os.File) *normalizedLineReader {
	return &normalizedLineReader{reader: bufio.NewReader(file)}
}

func (r *normalizedLineReader) Next() (string, bool, error) {
	if r.emittingBlanks > 0 {
		r.emittingBlanks--
		return "", true, nil
	}
	if r.hasReadyLine {
		r.hasReadyLine = false
		return r.readyLine, true, nil
	}
	if r.done {
		return "", false, nil
	}
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", false, err
		}
		line = strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if line == "" {
			r.pendingBlanks++
		} else {
			r.readyLine = line
			r.hasReadyLine = true
			r.emittingBlanks = r.pendingBlanks
			r.pendingBlanks = 0
			return r.Next()
		}
		if err != nil {
			r.done = true
			return "", false, nil
		}
	}
}

type tokenReader struct {
	reader *bufio.Reader
}

func newTokenReader(file *os.File) *tokenReader {
	return &tokenReader{reader: bufio.NewReader(file)}
}

func (r *tokenReader) Next() (string, bool, error) {
	var token strings.Builder
	started := false
	for {
		character, _, err := r.reader.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return token.String(), started, nil
			}
			return "", false, err
		}
		if unicode.IsSpace(character) {
			if started {
				return token.String(), true, nil
			}
			continue
		}
		started = true
		token.WriteRune(character)
	}
}

func normalizeOutput(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	lines := strings.Split(output, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
