package api

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"webclash/internal/util"
)

var kernelVersionRe = regexp.MustCompile(`^(latest|v?[\w.-]+)$`)

func (s *Server) handleKernelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.core.KernelStatus())
}

func (s *Server) handleKernelLogs(w http.ResponseWriter, r *http.Request) {
	lines := 200.0
	if vals, ok := r.URL.Query()["lines"]; ok && len(vals) > 0 {
		n := util.JsNumber(vals[0])
		if !math.IsNaN(n) && !math.IsInf(n, 0) {
			lines = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.core.Kernel.RecentLogs(int(lines))})
}

func (s *Server) handleKernelStart(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Kernel.Start(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.core.KernelStatus())
}

func (s *Server) handleKernelStop(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Kernel.Stop(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.core.KernelStatus())
}

func (s *Server) handleKernelRestart(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Kernel.Restart(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.core.KernelStatus())
}

func (s *Server) handleKernelCheckUpdate(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	version := s.core.Settings().Kernel.Version
	if m, ok := body.(map[string]any); ok {
		if v, ok := m["version"].(string); ok && strings.TrimSpace(v) != "" {
			version = strings.TrimSpace(v)
		}
	}
	result, err := s.core.Kernel.CheckUpdate(version)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleKernelDownload(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	version := s.core.Settings().Kernel.Version
	if m, ok := body.(map[string]any); ok {
		if v, ok := m["version"].(string); ok && strings.TrimSpace(v) != "" {
			version = strings.TrimSpace(v)
		}
	}
	if !kernelVersionRe.MatchString(version) {
		writeError(w, util.NewHttpError(400, "版本号格式不正确"))
		return
	}
	// Download runs in the background; the frontend polls /kernel/status.
	go func() {
		if err := s.core.Kernel.Download(version); err != nil {
			fmt.Fprintf(os.Stderr, "[webclash] 下载内核失败：%s\n", util.ErrorMessage(err))
		}
	}()
	time.Sleep(80 * time.Millisecond)
	writeJSON(w, http.StatusOK, s.core.KernelStatus())
}

func (s *Server) handleKernelCancelDownload(w http.ResponseWriter, r *http.Request) {
	if err := s.core.Kernel.CancelDownload(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.core.KernelStatus())
}
