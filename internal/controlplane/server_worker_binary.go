package controlplane

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) handleWorkerBinary(w http.ResponseWriter, r *http.Request) {
	if !constantToken(r.Header.Get("Authorization"), s.WorkerDownloadToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	path := s.BinaryPath
	if path == "" {
		path = "/proc/self/exe"
	}
	digest, err := s.workerBinaryDigest(path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "worker binary unavailable"})
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	w.Header().Set("X-Vessica-Worker-SHA256", digest)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "worker binary unavailable"})
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="ves"`)
	_, _ = io.Copy(w, file)
}

func (s *Server) workerBinaryDigest(path string) (string, error) {
	s.binaryDigestOnce.Do(func() {
		file, err := os.Open(filepath.Clean(path))
		if err != nil {
			s.binaryDigestErr = err
			return
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			s.binaryDigestErr = err
			return
		}
		s.binaryDigest = fmt.Sprintf("%x", hash.Sum(nil))
	})
	return s.binaryDigest, s.binaryDigestErr
}
