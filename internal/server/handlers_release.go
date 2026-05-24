package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxReleaseUploadBytes = 512 << 20

type releaseUploadResponse struct {
	Product    string `json:"product"`
	File       string `json:"file"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	UploadedAt string `json:"uploaded_at"`
}

func (a *App) handleAdminReleaseValidateURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONOrError(w, nil, methodNotAllowed())
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONOrError(w, nil, badRequest("bad_json", "bad json"))
		return
	}
	if err := validateDownloadURL(r, req.URL); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminReleaseUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONOrError(w, nil, methodNotAllowed())
		return
	}
	product := strings.TrimPrefix(r.URL.Path, "/api/admin/releases/")
	product = strings.TrimSuffix(product, "/upload")
	if product != "client" && product != "lan" {
		writeJSONOrError(w, nil, badRequest("bad_product", "product must be client or lan"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxReleaseUploadBytes)
	if err := r.ParseMultipartForm(maxReleaseUploadBytes); err != nil {
		writeJSONOrError(w, nil, badRequest("bad_upload", "bad upload"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONOrError(w, nil, badRequest("file_required", "file is required"))
		return
	}
	defer file.Close()
	name := safeReleaseFilename(header.Filename)
	if name == "" {
		writeJSONOrError(w, nil, badRequest("bad_filename", "bad filename"))
		return
	}
	dir := filepath.Join("uploads", "releases", product)
	if err := os.MkdirAll(dir, 0750); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	path := filepath.Join(dir, time.Now().UTC().Format("20060102150405")+"-"+name)
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	hash := sha256.New()
	_, copyErr := io.Copy(out, io.TeeReader(file, hash))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		writeJSONOrError(w, nil, copyErr)
		return
	}
	if closeErr != nil {
		_ = os.Remove(path)
		writeJSONOrError(w, nil, closeErr)
		return
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	path = filepath.Clean(path)
	url := requestBaseURL(r) + "/downloads/" + product + "/installer"
	if err := a.persistReleaseUpload(r, product, path, sha); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	_ = a.db.Audit(r.Context(), "release_upload", product+" "+path)
	writeJSON(w, http.StatusOK, releaseUploadResponse{
		Product:    product,
		File:       path,
		URL:        url,
		SHA256:     sha,
		UploadedAt: time.Now().Format(time.RFC3339),
	})
}

func validateDownloadURL(r *http.Request, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return badRequest("bad_release_url", "release url must be http or https")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodHead, raw, nil)
	if err != nil {
		return badRequest("bad_release_url", "release url is invalid")
	}
	res, err := client.Do(req)
	if err != nil || res.StatusCode == http.StatusMethodNotAllowed {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		req, reqErr := http.NewRequestWithContext(r.Context(), http.MethodGet, raw, nil)
		if reqErr != nil {
			return badRequest("bad_release_url", "release url is invalid")
		}
		req.Header.Set("Range", "bytes=0-0")
		res, err = client.Do(req)
	}
	if err != nil {
		return badRequest("release_url_unreachable", "release url is unreachable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return badRequest("release_url_unreachable", "release url is unreachable")
	}
	return nil
}

func (a *App) persistReleaseUpload(r *http.Request, product, path, sha string) error {
	a.cfgMu.Lock()
	if product == "client" {
		a.cfg.ClientReleaseFile = path
		a.cfg.ClientReleaseURL = ""
		a.cfg.ClientReleaseSHA256 = sha
	} else {
		a.cfg.LANReleaseFile = path
		a.cfg.LANReleaseURL = ""
		a.cfg.LANReleaseSHA256 = sha
	}
	a.cfgMu.Unlock()
	settings := map[string]string{}
	if product == "client" {
		settings[settingClientReleaseFile] = path
		settings[settingClientReleaseURL] = ""
		settings[settingClientReleaseSHA256] = sha
	} else {
		settings[settingLANReleaseFile] = path
		settings[settingLANReleaseURL] = ""
		settings[settingLANReleaseSHA256] = sha
	}
	for key, value := range settings {
		if err := a.db.PutSystemSetting(r.Context(), key, value); err != nil {
			return err
		}
	}
	return nil
}

func safeReleaseFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, ".-_")
	return name
}
