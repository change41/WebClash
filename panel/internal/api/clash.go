package api

import (
	"net/http"
	"strconv"

	"webclash/internal/mihomo"
	"webclash/internal/util"
)

var clashModes = map[string]bool{"rule": true, "global": true, "direct": true}

// clashClient returns the controller client, or writes 409 when the kernel is
// not running (port of routes/clash.ts client()).
func (s *Server) clashClient(w http.ResponseWriter) (*mihomo.Client, bool) {
	if !s.core.Kernel.IsRunning() {
		writeError(w, util.NewHttpError(409, "内核未运行，请先启动内核"))
		return nil, false
	}
	return s.core.Kernel.Client(), true
}

func requireQuery(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		writeError(w, util.NewHttpError(400, "缺少参数 "+key))
		return "", false
	}
	return v, true
}

func forwardClash(w http.ResponseWriter, res any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func enc(v string) string { return util.EncodeURIComponent(v) }

func jsNumString(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (s *Server) handleClashVersion(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/version", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashConfigsGet(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/configs", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashConfigsPatch(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	if mode, ok := m["mode"].(string); ok {
		if !clashModes[mode] {
			writeError(w, util.NewHttpError(400, "不支持的代理模式"))
			return
		}
		if err := s.core.ApplyMode(mode); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode})
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	if _, err := c.Request("PATCH", "/configs", m, 15000); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClashProxies(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/proxies", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashSelect(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	group, gok := m["group"].(string)
	name, nok := m["name"].(string)
	if !gok || !nok {
		writeError(w, util.NewHttpError(400, "缺少 group 或 name"))
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	if _, err := c.Request("PUT", "/proxies/"+enc(group), map[string]any{"name": name}, 15000); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// delayParams resolves the test url/timeout from query or the active profile.
func (s *Server) delayParams(r *http.Request) (string, float64) {
	test := s.core.TestConfig()
	q := r.URL.Query()
	url := test.TestUrl
	if u := q.Get("url"); u != "" {
		url = u
	}
	timeout := float64(test.TestTimeout)
	if n := util.JsNumber(q.Get("timeout")); n > 0 {
		timeout = n
	}
	return url, timeout
}

func (s *Server) handleClashProxyDelay(w http.ResponseWriter, r *http.Request) {
	name, ok := requireQuery(w, r, "name")
	if !ok {
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	url, timeout := s.delayParams(r)
	path := "/proxies/" + enc(name) + "/delay?timeout=" + jsNumString(timeout) + "&url=" + enc(url)
	res, err := c.Request("GET", path, nil, int(timeout)+10000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashGroupDelay(w http.ResponseWriter, r *http.Request) {
	name, ok := requireQuery(w, r, "name")
	if !ok {
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	url, timeout := s.delayParams(r)
	path := "/group/" + enc(name) + "/delay?timeout=" + jsNumString(timeout) + "&url=" + enc(url)
	res, err := c.Request("GET", path, nil, int(timeout)+15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashProviders(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/providers/proxies", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashProviderUpdate(w http.ResponseWriter, r *http.Request) {
	body, err := readJSONBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, _ := body.(map[string]any)
	name, _ := m["name"].(string)
	if name == "" {
		writeError(w, util.NewHttpError(400, "缺少参数 name"))
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	if _, err := c.Request("PUT", "/providers/proxies/"+enc(name), nil, 60000); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClashProviderHealthcheck(w http.ResponseWriter, r *http.Request) {
	name, ok := requireQuery(w, r, "name")
	if !ok {
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/providers/proxies/"+enc(name)+"/healthcheck", nil, 60000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashConnectionsGet(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/connections", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashConnectionsDelete(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	if _, err := c.Request("DELETE", "/connections", nil, 15000); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClashConnectionDelete(w http.ResponseWriter, r *http.Request) {
	id := PathParam(r, "id")
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	if _, err := c.Request("DELETE", "/connections/"+enc(id), nil, 15000); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClashRules(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/rules", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashRuleProviders(w http.ResponseWriter, r *http.Request) {
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	res, err := c.Request("GET", "/providers/rules", nil, 15000)
	forwardClash(w, res, err)
}

func (s *Server) handleClashRuleProviderContent(w http.ResponseWriter, r *http.Request) {
	name, ok := requireQuery(w, r, "name")
	if !ok {
		return
	}
	res, err := s.core.RuleProviderContent(name)
	forwardClash(w, res, err)
}

func (s *Server) handleClashDNSQuery(w http.ResponseWriter, r *http.Request) {
	name, ok := requireQuery(w, r, "name")
	if !ok {
		return
	}
	c, ok := s.clashClient(w)
	if !ok {
		return
	}
	typ := "A"
	if vals, ok := r.URL.Query()["type"]; ok && len(vals) > 0 {
		typ = vals[0]
	}
	res, err := c.Request("GET", "/dns/query?name="+enc(name)+"&type="+enc(typ), nil, 20000)
	forwardClash(w, res, err)
}
