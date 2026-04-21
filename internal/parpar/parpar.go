package parpar

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"go-post-tools/internal/binutil"
	"go-post-tools/internal/config"
)

type Progress struct {
	Percent float64 `json:"percent"`
	Done    bool    `json:"done"`
	Error   string  `json:"error,omitempty"`
}

var percentRegex = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)

func binaryPath() string {
	if path, err := binutil.ExtractBinary("parpar"); err == nil {
		return path
	}
	if path, err := exec.LookPath("parpar"); err == nil {
		return path
	}
	return "parpar"
}

func Run(ctx context.Context, cfg *config.Config, inputPath string, onProgress func(Progress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ext := filepath.Ext(inputPath)
	base := inputPath[:len(inputPath)-len(ext)]
	outPath := base + ".par2"

	sliceSize := cfg.ParParSliceSize
	if sliceSize <= 0 {
		sliceSize = 768000
	}
	redundancy := cfg.ParParRedundancy
	if redundancy <= 0 {
		redundancy = 5
	}
	threads := cfg.ParParThreads
	if threads <= 0 {
		threads = 8
	}

	args := []string{
		"-s", strconv.Itoa(sliceSize) + "B",
		"-r", fmt.Sprintf("%.0f%%", redundancy),
		"-t", strconv.Itoa(threads),
		"-o", outPath,
		"--", inputPath,
	}

	cmd := exec.CommandContext(ctx, binaryPath(), args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("pipe stderr: %w", err)
	}
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("démarrage parpar: %w", err)
	}

	parseProgress(stderr, onProgress)

	if err := cmd.Wait(); err != nil {
		onProgress(Progress{Done: true, Error: err.Error()})
		return fmt.Errorf("parpar: %w", err)
	}

	onProgress(Progress{Percent: 100, Done: true})
	return nil
}

func parseProgress(r io.Reader, onProgress func(Progress)) {
	scanner := bufio.NewScanner(r)
	scanner.Split(scanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if m := percentRegex.FindStringSubmatch(line); len(m) >= 2 {
			if pct, err := strconv.ParseFloat(m[1], 64); err == nil {
				onProgress(Progress{Percent: pct})
			}
		}
	}
}

func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[:i], nil
		}
		// ANSI escape sequence (cursor home)
		if data[i] == 0x1b && i+2 < len(data) && data[i+1] == '[' {
			for j := i + 2; j < len(data); j++ {
				if (data[j] >= 'A' && data[j] <= 'Z') || (data[j] >= 'a' && data[j] <= 'z') {
					if i > 0 {
						return j + 1, data[:i], nil
					}
					i = j
					break
				}
			}
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
