package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"dsync/internal/config"
)

func TestImageHandlerOnlyServesHistoryImages(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "shot.png")
	os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nfake"), 0o644)
	os.WriteFile(filepath.Join(dir, "history.json"),
		[]byte(`[{"id":1,"peerId":"p","file":{"name":"shot.png","size":12,"path":"`+filepath.ToSlash(img)+`","status":"done"}}]`), 0o600)
	app, _ = NewApp(cfg) // reload history

	h := app.imageHandler()
	for path, want := range map[string]int{
		"/image/1":                http.StatusOK,
		"/image/2":                http.StatusNotFound,
		"/image/abc":              http.StatusNotFound,
		"/image/../../etc/passwd": http.StatusNotFound,
		"/other/1":                http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}
