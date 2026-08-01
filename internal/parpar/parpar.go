package parpar

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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

// Run génère les .par2 pour un fichier OU un dossier. Si inputPath est un dossier,
// les .par2 sont nommés d'après le dossier (ex: /path/MyShow.S01/ → /path/MyShow.S01.par2)
// et couvrent tous les fichiers du dossier (récursif). Pour un fichier single, c'est
// l'ancien comportement (par2 nommés d'après le file basename).
//
// Retourne le path du .par2 principal généré (utile au caller pour le globbing).
func Run(ctx context.Context, cfg *config.Config, inputPath string, onProgress func(Progress)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stat, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("stat input: %w", err)
	}

	// Calcule le par2 output path et la liste des inputs à passer à parpar
	var outPath string
	var inputs []string
	if stat.IsDir() {
		// Folder : par2 nommés d'après le dossier, walk récursif pour les fichiers
		base := strings.TrimRight(inputPath, string(filepath.Separator))
		outPath = base + ".par2"
		walkErr := filepath.Walk(inputPath, func(p string, info os.FileInfo, e error) error {
			if e != nil {
				return e
			}
			if !info.IsDir() {
				inputs = append(inputs, p)
			}
			return nil
		})
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", inputPath, walkErr)
		}
		if len(inputs) == 0 {
			return fmt.Errorf("dossier vide: %s", inputPath)
		}
	} else {
		ext := filepath.Ext(inputPath)
		base := inputPath[:len(inputPath)-len(ext)]
		outPath = base + ".par2"
		inputs = []string{inputPath}
	}

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

	// Auto-scale de la slice size pour éviter les crashs parpar sur gros REMUX :
	// la spec PAR2 plafonne à 65 535 slices/set. Au-delà de ~32 768 slices (moitié
	// de la limite, marge de sécurité), parpar plante fréquemment (OOM, exit 1).
	// Cible pratique : ~4000 slices, aligné sur 4 KB pour la perf disque.
	var totalSize int64
	for _, p := range inputs {
		if info, err := os.Stat(p); err == nil {
			totalSize += info.Size()
		}
	}
	const maxSlices = 32768
	const targetSlices = 4000
	if totalSize > 0 && totalSize/int64(sliceSize) > maxSlices {
		newSlice := totalSize / targetSlices
		if r := newSlice % 4096; r != 0 {
			newSlice += 4096 - r
		}
		onProgress(Progress{Error: fmt.Sprintf("slice size auto-ajustée: %d B → %d B (input=%.1f GB, cible ~%d slices)", sliceSize, newSlice, float64(totalSize)/1e9, targetSlices)})
		sliceSize = int(newSlice)
	}

	args := []string{
		"-s", strconv.Itoa(sliceSize) + "B",
		"-r", fmt.Sprintf("%.0f%%", redundancy),
		"-t", strconv.Itoa(threads),
		"-o", outPath,
		"--overwrite", // écrase les .par2 laissés par une run précédente (sinon EEXIST)
		"--",
	}
	args = append(args, inputs...)

	cmd := exec.CommandContext(ctx, binaryPath(), args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("pipe stderr: %w", err)
	}
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("démarrage parpar: %w", err)
	}

	errLines := parseProgress(stderr, onProgress)

	if err := cmd.Wait(); err != nil {
		tail := lastLines(errLines, 20)
		msg := strings.Join(tail, "\n")
		onProgress(Progress{Done: true, Error: err.Error() + "\n" + msg})
		return fmt.Errorf("parpar: %w\n%s", err, msg)
	}

	onProgress(Progress{Percent: 100, Done: true})
	return nil
}

func lastLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

func parseProgress(r io.Reader, onProgress func(Progress)) []string {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	scanner.Split(scanLines)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if m := percentRegex.FindStringSubmatch(line); len(m) >= 2 {
			if pct, err := strconv.ParseFloat(m[1], 64); err == nil {
				onProgress(Progress{Percent: pct})
			}
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
