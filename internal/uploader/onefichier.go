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
