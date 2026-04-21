package nyuu

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"go-post-tools/internal/binutil"
	"go-post-tools/internal/config"
)

type Progress struct {
	Percent  float64 `json:"percent"`
	Articles string  `json:"articles"`
	Speed    string  `json:"speed"`
	ETA      string  `json:"eta"`
	Done     bool    `json:"done"`
	Error    string  `json:"error,omitempty"`
}

type Result struct {
	NZBPath string `json:"nzb_path"`
}

var (
	progressRegex = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	articlesRegex = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)
	speedRegex    = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([KMGT]i?B/s|B/s)`)
	etaRegex      = regexp.MustCompile(`ETA\s+(\d{2}:\d{2}(?::\d{2})?)`)
)

func binaryPath() string {
	if path, err := binutil.ExtractBinary("nyuu"); err == nil {
		return path
	}
	if path, err := exec.LookPath("nyuu"); err == nil {
		return path
	}
	return "nyuu"
}

func Run(ctx context.Context, cfg *config.Config, inputFiles []string, nzbOutputPath string, releaseName string, onProgress func(Progress)) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	args := buildArgs(cfg, inputFiles, nzbOutputPath, releaseName)
	cmd := exec.CommandContext(ctx, binaryPath(), args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe stderr: %w", err)
	}
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("démarrage nyuu: %w", err)
	}

	errLines := parseProgress(stderr, onProgress)

	if err := cmd.Wait(); err != nil {
		msg := strings.Join(errLines, "\n")
		onProgress(Progress{Done: true, Error: msg})
		return nil, fmt.Errorf("nyuu: %w\n%s", err, msg)
	}

	onProgress(Progress{Percent: 100, Done: true})
	return &Result{NZBPath: nzbOutputPath}, nil
}

func buildArgs(cfg *config.Config, inputFiles []string, nzbOutputPath string, releaseName string) []string {
	group := cfg.UsenetGroup
	if group == "" {
		group = "alt.binaries.test"
	}
	conns := cfg.UsenetConns
	if conns <= 0 {
		conns = 20
	}

	args := []string{
		"-h", cfg.UsenetHost,
		"-P", strconv.Itoa(cfg.UsenetPort),
		"-u", cfg.UsenetUser,
		"-p", cfg.UsenetPassword,
		"-n", strconv.Itoa(conns),
		"-g", group,
		"-o", nzbOutputPath,
		"--nzb-title", releaseName,
		"-f", "{rand(14)} {rand(14)}@{rand(5)}.{rand(3)}",
		"--message-id", "{rand(32)}@{rand(8)}.{rand(3)}",
		"--subject", "{rand(32)}",
		"--nzb-subject", `[{0filenum}/{files}] - "{filename}" yEnc ({part}/{parts})`,
		"--obfuscate-articles",
		"--overwrite",
		"--progress=stderr",
	}

	if cfg.UsenetSSL {
		args = append(args, "-S")
	}

	args = append(args, inputFiles...)
	return args
}

func parseProgress(r io.Reader, onProgress func(Progress)) []string {
	scanner := bufio.NewScanner(r)
	scanner.Split(scanLines)

	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lines = append(lines, line)

		p := Progress{}
		updated := false

		if m := progressRegex.FindStringSubmatch(line); len(m) >= 2 {
			if pct, err := strconv.ParseFloat(m[1], 64); err == nil {
				p.Percent = pct
				updated = true
			}
		}
		if m := articlesRegex.FindStringSubmatch(line); len(m) >= 3 {
			p.Articles = m[1] + "/" + m[2]
			updated = true
		}
		if m := speedRegex.FindStringSubmatch(line); len(m) >= 3 {
			p.Speed = m[1] + " " + m[2]
			updated = true
		}
		if m := etaRegex.FindStringSubmatch(line); len(m) >= 2 {
			p.ETA = m[1]
			updated = true
		}
		if updated {
			onProgress(p)
		}
	}
	return lines
}

func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[:i], nil
		}
		if data[i] == 0x1b && i+3 < len(data) && data[i+1] == '[' {
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
