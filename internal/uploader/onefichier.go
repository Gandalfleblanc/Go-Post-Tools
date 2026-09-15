package uploader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

type OneFichierResult struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

// oneFichierAPI : petit helper interne pour taper l'API 1F en JSON.
func oneFichierAPI(ctx context.Context, apiKey, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.1fichier.com/v1"+path, reqBody)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("1F %s HTTP %d: %s", path, resp.StatusCode, truncateStr(string(data), 200))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// FolderAPIUnsupportedError : renvoyée quand le compte 1F ne supporte pas
// les endpoints /folder/* (tier restreint, clé fraîche, IP-lock côté user).
// L'upload lui-même a bien réussi — juste le rangement dossier qui ne
// passe pas. Message court pour affichage frontend.
type FolderAPIUnsupportedError struct{ Raw string }

func (e *FolderAPIUnsupportedError) Error() string {
	return "compte 1F non compatible avec la gestion de dossiers via API"
}

// isFolderAPIUnsupported : True quand 1F retourne « No such user » sur un
// endpoint /folder/*. Reste inexpliqué côté doc mais empirique.
func isFolderAPIUnsupported(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such user") || strings.Contains(s, "http 403")
}

// EnsureOneFichierFolder : cherche ou crée un dossier à la racine du compte
// 1Fichier. Retourne son folder_id. Tolère le cas où /folder/ls.cgi est
// refusé pour certains tiers : tente alors mkdir direct.
func EnsureOneFichierFolder(ctx context.Context, apiKey, name string) (int, error) {
	if apiKey == "" || name == "" {
		return 0, nil
	}
	// Tentative 1 : list racine (folder_id = 0) puis chercher par nom.
	var lsResp struct {
		Status     string `json:"status"`
		SubFolders []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"sub_folders"`
	}
	lsErr := oneFichierAPI(ctx, apiKey, "/folder/ls.cgi", map[string]any{"folder_id": 0}, &lsResp)
	if lsErr == nil {
		for _, f := range lsResp.SubFolders {
			if f.Name == name {
				return f.ID, nil
			}
		}
	}
	// Tentative 2 : mkdir direct. Si le dossier existe déjà, 1F peut renvoyer
	// une erreur (« folder already exists ») avec l'ID existant, ou juste
	// l'ID. On accepte les deux cas.
	var mkResp struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		FolderID int    `json:"folder_id"`
	}
	mkErr := oneFichierAPI(ctx, apiKey, "/folder/mkdir.cgi", map[string]any{"folder_id": 0, "name": name}, &mkResp)
	if mkErr == nil && mkResp.FolderID > 0 {
		return mkResp.FolderID, nil
	}
	// Les 2 tentatives ont échoué. Si /ls a échoué avec « No such user » ou
	// 403, c'est très probablement un tier 1F qui ne supporte pas l'API
	// dossier — on remonte une erreur typée pour un log clair côté app.
	if isFolderAPIUnsupported(lsErr) || isFolderAPIUnsupported(mkErr) {
		raw := ""
		if lsErr != nil {
			raw = lsErr.Error()
		} else if mkErr != nil {
			raw = mkErr.Error()
		}
		return 0, &FolderAPIUnsupportedError{Raw: raw}
	}
	if lsErr != nil {
		return 0, lsErr
	}
	return 0, mkErr
}

// MoveToOneFichierFolder : déplace un ou plusieurs fichiers 1F vers un dossier.
func MoveToOneFichierFolder(ctx context.Context, apiKey string, fileURLs []string, folderID int) error {
	if apiKey == "" || folderID <= 0 || len(fileURLs) == 0 {
		return nil
	}
	return oneFichierAPI(ctx, apiKey, "/file/mv.cgi", map[string]any{
		"urls":                  fileURLs,
		"destination_folder_id": folderID,
	}, nil)
}

func UploadOneFichier(ctx context.Context, apiKey, filePath string, onProgress func(UploadProgress)) (*OneFichierResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c15 := &http.Client{Timeout: 15 * time.Second}

	// 1. Obtenir le serveur d'upload
	reqServer, _ := http.NewRequestWithContext(ctx, "GET", "https://api.1fichier.com/v1/upload/get_upload_server.cgi", nil)
	reqServer.Header.Set("Authorization", "Bearer "+apiKey)
	reqServer.Header.Set("Content-Type", "application/json")
	resp, err := c15.Do(reqServer)
	if err != nil {
		return nil, fmt.Errorf("obtention serveur: %w", err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var serverResp struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	if err := json.Unmarshal(rawBody, &serverResp); err != nil || serverResp.URL == "" {
		return nil, fmt.Errorf("réponse serveur invalide (HTTP %d): %s", resp.StatusCode, string(rawBody))
	}

	uploadURL := "https://" + serverResp.URL + "/upload.cgi?id=" + serverResp.ID

	// 2. Préparer le fichier
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("ouverture fichier: %w", err)
	}
	defer f.Close()

	info, _ := f.Stat()
	totalSize := info.Size()

	// Boundary fixe pour calculer le Content-Length exact
	boundary := strconv.FormatInt(rand.Int63(), 16)
	var measure bytes.Buffer
	wm := multipart.NewWriter(&measure)
	_ = wm.SetBoundary(boundary)
	_, _ = wm.CreateFormFile("file[]", filepath.Base(filePath))
	wm.Close()
	contentLength := int64(measure.Len()) + totalSize

	// Pipe : goroutine écrit, HTTP client lit via progressReader
	pr, pw := io.Pipe()
	w := multipart.NewWriter(pw)
	_ = w.SetBoundary(boundary)

	go func() {
		part, err := w.CreateFormFile("file[]", filepath.Base(filePath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		buf := make([]byte, 256*1024)
		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				if _, werr := part.Write(buf[:n]); werr != nil {
					pw.CloseWithError(werr)
					return
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				pw.CloseWithError(readErr)
				return
			}
		}
		w.Close()
		pw.Close()
	}()

	// progressReader sur le côté réseau (ce que le HTTP client lit réellement)
	body := io.Reader(pr)
	if onProgress != nil {
		body = newProgressReader(pr, contentLength, onProgress)
	}

	noRedirect := &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	reqUp, _ := http.NewRequestWithContext(ctx, "POST", uploadURL, body)
	reqUp.Header.Set("Content-Type", w.FormDataContentType())
	reqUp.Header.Set("Authorization", "Bearer "+apiKey)
	// JSON: 1 pour forcer 1Fichier à répondre en JSON au lieu de HTML (sinon
	// on récupère une page d'erreur HTML impossible à parser).
	reqUp.Header.Set("JSON", "1")
	reqUp.Header.Set("Accept", "application/json")
	reqUp.ContentLength = contentLength

	upResp, err := noRedirect.Do(reqUp)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	upBody, _ := io.ReadAll(upResp.Body)
	upResp.Body.Close()

	// 100% une fois la réponse reçue
	if onProgress != nil {
		onProgress(UploadProgress{Percent: 100})
	}

	// Étape 1 : chercher les links dans la réponse d'upload directement.
	// 1Fichier renvoie parfois {"links":[...]} en JSON dès l'upload, sans
	// besoin de rappeler end.pl. Sinon on parse l'HTML pour extraire les URLs
	// de partage (motif https://1fichier.com/?<token>).
	type linkOut struct {
		Download string `json:"download"`
		Filename string `json:"filename"`
	}
	tryParseLinks := func(body []byte) *OneFichierResult {
		// JSON path
		var jr struct {
			Links []linkOut `json:"links"`
		}
		if err := json.Unmarshal(body, &jr); err == nil && len(jr.Links) > 0 {
			return &OneFichierResult{URL: jr.Links[0].Download, Filename: jr.Links[0].Filename}
		}
		// HTML fallback : extrait une URL share 1Fichier
		bs := string(body)
		if idx := strings.Index(bs, "https://1fichier.com/?"); idx >= 0 {
			end := idx + len("https://1fichier.com/?")
			for end < len(bs) {
				c := bs[end]
				if c == '"' || c == '<' || c == ' ' || c == '\n' || c == '\r' {
					break
				}
				end++
			}
			return &OneFichierResult{URL: bs[:end][idx:], Filename: filepath.Base(filePath)}
		}
		return nil
	}
	if r := tryParseLinks(upBody); r != nil {
		return r, nil
	}

	// Étape 2 : suivre un redirect 302 vers /end.pl?xid=<REAL_XID>. Le xid du
	// Location diffère de serverResp.ID (qui n'est qu'un id de requête initial).
	xid := serverResp.ID
	if loc := upResp.Header.Get("Location"); loc != "" {
		if idx := strings.Index(loc, "xid="); idx >= 0 {
			xid = loc[idx+len("xid="):]
			if amp := strings.Index(xid, "&"); amp >= 0 {
				xid = xid[:amp]
			}
		}
	}
	if upResp.StatusCode >= 400 {
		return nil, fmt.Errorf("upload 1Fichier HTTP %d (aucun lien détecté dans le body) : %s", upResp.StatusCode, truncateStr(string(upBody), 400))
	}

	endURL := "https://" + serverResp.URL + "/end.pl?xid=" + xid
	reqEnd, _ := http.NewRequestWithContext(ctx, "GET", endURL, nil)
	reqEnd.Header.Set("JSON", "1")
	reqEnd.Header.Set("Authorization", "Bearer "+apiKey)
	reqEnd.Header.Set("Content-Type", "application/json")

	endResp, err := c15.Do(reqEnd)
	if err != nil {
		return nil, fmt.Errorf("récupération liens: %w", err)
	}
	endBody, _ := io.ReadAll(endResp.Body)
	endResp.Body.Close()

	if r := tryParseLinks(endBody); r != nil {
		return r, nil
	}

	// Dernier échec : cherche les messages d'erreur typiques 1Fichier :
	//   - <div class="ct_warn">MESSAGE</div>  (erreur générique)
	//   - <div class="alerte">MESSAGE</div>   (alerte)
	//   - JSON {"message":"..."}              (parfois retourné en HTML)
	extractErr := func(body []byte) string {
		s := string(body)
		// Cherche dans plusieurs conteneurs d'erreur possibles
		for _, marker := range []string{`class="ct_warn"`, `class="alerte"`, `class="ct_err"`, `class="error"`} {
			if idx := strings.Index(s, marker); idx >= 0 {
				rest := s[idx:]
				if open := strings.Index(rest, ">"); open >= 0 {
					rest = rest[open+1:]
					if end := strings.Index(rest, "</div>"); end >= 0 {
						return strings.TrimSpace(rest[:end])
					}
				}
			}
		}
		// Titre de la page (souvent "1fichier.com: <erreur>")
		if idx := strings.Index(s, "<title>"); idx >= 0 {
			t := s[idx+7:]
			if end := strings.Index(t, "</title>"); end >= 0 {
				title := strings.TrimSpace(t[:end])
				if !strings.Contains(strings.ToLower(title), "cloud storage") {
					return "title=" + title
				}
			}
		}
		// Body length + preview
		return fmt.Sprintf("len=%d preview=%s", len(body), truncateStr(strings.ReplaceAll(string(body), "\n", " "), 400))
	}
	return nil, fmt.Errorf("1Fichier a rejeté l'upload (HTTP %d) — %s · end.pl: %s",
		upResp.StatusCode, extractErr(upBody), extractErr(endBody))
}
