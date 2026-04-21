package uploader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type SendCmResult struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

func UploadSendCm(ctx context.Context, apiKey, filePath string, onProgress func(UploadProgress)) (*SendCmResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c15 := &http.Client{Timeout: 15 * time.Second}

	// 1. Obtenir le serveur d'upload
	reqServer, _ := http.NewRequestWithContext(ctx, "GET", "https://send.cm/api/upload/server?key="+apiKey, nil)
	resp, err := c15.Do(reqServer)
	if err != nil {
		return nil, fmt.Errorf("obtention serveur: %w", err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var serverResp struct {
		Result string `json:"result"`
		SessID string `json:"sess_id"`
	}
	if err := json.Unmarshal(rawBody, &serverResp); err != nil || serverResp.Result == "" {
		return nil, fmt.Errorf("réponse serveur invalide (HTTP %d): %s", resp.StatusCode, string(rawBody))
	}

	// 2. Préparer le fichier
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("ouverture fichier: %w", err)
	}
	defer f.Close()

	info, _ := f.Stat()
	totalSize := info.Size()

	// Pipe : goroutine écrit, HTTP client lit via progressReader
	pr, pw := io.Pipe()
	w := multipart.NewWriter(pw)

	go func() {
		_ = w.WriteField("api_key", apiKey)
		if serverResp.SessID != "" {
			_ = w.WriteField("sess_id", serverResp.SessID)
		}
		part, err := w.CreateFormFile("file", filepath.Base(filePath))
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

	// progressReader sur le côté réseau — total approximatif (fileSize suffit ici)
	body := io.Reader(pr)
	if onProgress != nil {
		body = newProgressReader(pr, totalSize, onProgress)
	}

	reqUp, _ := http.NewRequestWithContext(ctx, "POST", serverResp.Result, body)
	reqUp.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 0}
	upResp, err := client.Do(reqUp)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	upBody, _ := io.ReadAll(upResp.Body)
	upResp.Body.Close()

	// 100% une fois la réponse reçue
	if onProgress != nil {
		onProgress(UploadProgress{Percent: 100})
	}

	var uploadResp []struct {
		FileCode   string `json:"file_code"`
		FileStatus string `json:"file_status"`
	}
	if err := json.Unmarshal(upBody, &uploadResp); err != nil || len(uploadResp) == 0 {
		return nil, fmt.Errorf("réponse upload invalide: %s", string(upBody))
	}
	if uploadResp[0].FileStatus != "OK" {
		return nil, fmt.Errorf("send.cm status: %s", uploadResp[0].FileStatus)
	}

	url := "https://send.cm/" + uploadResp[0].FileCode
	return &SendCmResult{
		URL:      url,
		Filename: uploadResp[0].FileCode,
	}, nil
}
