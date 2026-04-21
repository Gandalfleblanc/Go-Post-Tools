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
	"time"
)

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
	reqUp.ContentLength = contentLength

	upResp, err := noRedirect.Do(reqUp)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	upResp.Body.Close()

	// 100% une fois la réponse reçue
	if onProgress != nil {
		onProgress(UploadProgress{Percent: 100})
	}

	// 3. Récupérer les liens via end.pl
	endURL := "https://" + serverResp.URL + "/end.pl?xid=" + serverResp.ID
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

	var endResult struct {
		Links []struct {
			Download string `json:"download"`
			Filename string `json:"filename"`
		} `json:"links"`
	}
	if err := json.Unmarshal(endBody, &endResult); err != nil || len(endResult.Links) == 0 {
		return nil, fmt.Errorf("réponse end.pl invalide: %s", string(endBody))
	}

	return &OneFichierResult{
		URL:      endResult.Links[0].Download,
		Filename: endResult.Links[0].Filename,
	}, nil
}
