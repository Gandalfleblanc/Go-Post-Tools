package binutil

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	binaries embed.FS
	initDone bool
	mu       sync.Mutex
	cacheDir string
	cacheErr error
	cacheOnce sync.Once
)

func Init(embedded embed.FS) {
	mu.Lock()
	defer mu.Unlock()
	binaries = embedded
	initDone = true
}

func ensureCacheDir() (string, error) {
	cacheOnce.Do(func() {
		home, e := os.UserHomeDir()
		if e != nil {
			cacheErr = e
			return
		}
		cacheDir = filepath.Join(home, ".cache", "go-post-tools", "bin")
		cacheErr = os.MkdirAll(cacheDir, 0755)
	})
	return cacheDir, cacheErr
}

// ExtractBinary extrait un binaire embarqué vers le cache et retourne son chemin.
// Retourne une erreur si le binaire n'est pas embarqué.
func ExtractBinary(name string) (string, error) {
	mu.Lock()
	ok := initDone
	mu.Unlock()
	if !ok {
		return "", fmt.Errorf("binutil non initialisé")
	}

	dir, err := ensureCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}

	binName := name
	if runtime.GOOS == "windows" {
		binName = name + ".exe"
	}
	destPath := filepath.Join(dir, binName)

	// Déjà extrait
	if info, err := os.Stat(destPath); err == nil && info.Size() > 1024 {
		return destPath, nil
	}

	platform := runtime.GOOS + "-" + runtime.GOARCH
	candidates := []string{
		"binaries/" + platform + "/" + binName,
		"binaries/" + binName,
	}

	var data []byte
	for _, p := range candidates {
		data, err = fs.ReadFile(binaries, p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("binaire %s non embarqué", name)
	}

	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return "", fmt.Errorf("extraction %s: %w", name, err)
	}
	return destPath, nil
}
