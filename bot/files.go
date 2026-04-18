package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// tgDocument represents a Telegram document attachment.
type tgDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}

// tgPhoto represents one size of a Telegram photo.
type tgPhoto struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// tgFile is the response from getFile.
type tgFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
}

// downloadFile downloads a Telegram file to the user's workspace.
func (b *Bot) downloadFile(ctx context.Context, fileID, fileName, workspace string) (string, error) {
	// Step 1: get the file path from Telegram.
	params := url.Values{}
	params.Set("file_id", fileID)
	raw, err := b.api(ctx, "getFile", params)
	if err != nil {
		return "", fmt.Errorf("getFile: %w", err)
	}
	var f tgFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", fmt.Errorf("decode file: %w", err)
	}

	// Step 2: download the file content.
	dlURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", b.token, f.FilePath)
	req, err := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Step 3: save to workspace.
	destPath := filepath.Join(workspace, fileName)
	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("save file: %w", err)
	}

	return fmt.Sprintf("Saved %s (%d bytes)", fileName, n), nil
}

// sendFile sends a file from the workspace to a Telegram chat.
func (b *Bot) sendFile(ctx context.Context, chatID int64, workspace, filePath string) error {
	// Resolve relative to workspace.
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(workspace, filePath)
	}

	// Security: ensure the resolved path is within the workspace.
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	wsAbs, _ := filepath.Abs(workspace)
	if !strings.HasPrefix(absPath, wsAbs+"/") && absPath != wsAbs {
		return fmt.Errorf("path must be within workspace")
	}

	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", filePath)
	}
	// Telegram limit: 50MB for bot uploads.
	if info.Size() > 50*1024*1024 {
		return fmt.Errorf("file too large (%d MB, max 50 MB)", info.Size()/1024/1024)
	}

	// Build multipart form.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("chat_id", fmt.Sprintf("%d", chatID))

	part, err := w.CreateFormFile("document", filepath.Base(absPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	w.Close()

	endpoint := "https://api.telegram.org/bot" + b.token + "/sendDocument"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var r tgResp
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("decode sendDocument response: %w", err)
	}
	if !r.OK {
		return fmt.Errorf("sendDocument: %s", r.Description)
	}
	return nil
}

// listFiles returns a short listing of files in the workspace root.
func listFiles(workspace string) (string, error) {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "(empty workspace)", nil
	}
	var sb strings.Builder
	for _, e := range entries {
		info, _ := e.Info()
		if e.IsDir() {
			fmt.Fprintf(&sb, "📁 %s/\n", e.Name())
		} else if info != nil {
			fmt.Fprintf(&sb, "📄 %s (%d bytes)\n", e.Name(), info.Size())
		} else {
			fmt.Fprintf(&sb, "📄 %s\n", e.Name())
		}
	}
	return sb.String(), nil
}
