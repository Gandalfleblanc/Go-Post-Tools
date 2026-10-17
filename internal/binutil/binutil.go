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
	appVer   string
	mu       sync.Mutex
	cacheDir string
	cacheErr error
	cacheOnce sync.Once
)

func Init(embedded embed.FS, version string) {
	mu.Lock()
	defer mu.Unlock()
	binaries = embedded
	appVer = version
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
		if cacheErr = os.MkdirAll(cacheDir, 0755); cacheErr != nil {
			return
		}
		purgeStaleCache(cacheDir)
	})
	return cacheDir, cacheErr
}

// purgeStaleCache vide le cache quand l'app a changé de version. Sans ça, un
// binaire extrait une fois n'est JAMAIS remplacé (voir le test de taille dans
// ExtractBinary) : un utilisateur qui a reçu un binaire cassé le garde même
// après mise à jour. Cas réel : parpar x86_64 livré par erreur dans la build
// arm64, inutilisable dès que Rosetta n'est plus installé.
func purgeStaleCache(dir string) {
	mu.Lock()
	want := appVer
	mu.Unlock()
	if want == "" {
		return
	}
	marker := filepath.Join(dir, ".version")
	if got, err := os.ReadFile(marker); err == nil && string(got) == want {
		return
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, ent := range entries {
			if !ent.IsDir() {
				_ = os.Remove(filepath.Join(dir, ent.Name()))
			}
		}
	}
	_ = os.WriteFile(marker, []byte(want), 0644)
}

// ExtractBinary extrait un binaire embarqué vers le cache et retourne son chemin.
// Retourne une erreur si le binaire n'est pas embarqué.
//
// Si un dossier compagnon `binaries/<platform>/<name>.d/` existe (ex : DLLs
// Windows), tous ses fichiers sont extraits à côté du binaire principal.
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

	// Compagnons : binaries/<platform>/<name>.d/* → extraits dans le même
	// dossier cache que le binaire (ex : LIBCURL.DLL pour mediainfo Windows).
	companionDir := "binaries/" + platform + "/" + name + ".d"
	if entries, e := fs.ReadDir(binaries, companionDir); e == nil {
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			if cdata, ce := fs.ReadFile(binaries, companionDir+"/"+ent.Name()); ce == nil {
				_ = os.WriteFile(filepath.Join(dir, ent.Name()), cdata, 0644)
			}
		}
	}
	return destPath, nil
}
