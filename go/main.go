package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginName          = "xai-health-janitor"
	pluginVersion       = "0.1.0"
	resourcePath        = "/status"
	resourceContentType = "text/html; charset=utf-8"
	defaultModel        = "grok-4.5"
	defaultCLIVersion   = "0.1.220"
	defaultIntervalSec  = 300
	defaultBaseURL      = "https://cli-chat-proxy.grok.com/v1"
	defaultMgmtBase     = "http://127.0.0.1:8317"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      pluginapi.Metadata       `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

type registrationCapabilities struct {
	ManagementAPI bool `json:"management_api"`
}

type managementRegistration struct {
	Resources []managementResource `json:"resources,omitempty"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   []byte
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type pluginConfig struct {
	IntervalSeconds int      `yaml:"interval_seconds" json:"interval_seconds"`
	Model           string   `yaml:"model" json:"model"`
	CLIVersion      string   `yaml:"cli_version" json:"cli_version"`
	ManagementBase  string   `yaml:"management_base" json:"management_base"`
	ManagementKey   string   `yaml:"management_key" json:"management_key"`
	ProbeEnabled    *bool    `yaml:"probe_enabled" json:"probe_enabled"`
	AutoDelete      *bool    `yaml:"auto_delete" json:"auto_delete"`
	DryRun          bool     `yaml:"dry_run" json:"dry_run"`
	DeleteStatus    []int    `yaml:"delete_status_codes" json:"delete_status_codes"`
	Providers       []string `yaml:"providers" json:"providers"`
	Concurrency     int      `yaml:"concurrency" json:"concurrency"`
}

type authListResponse struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}

type xaiAuthFile struct {
	Type         string            `json:"type"`
	Email        string            `json:"email"`
	AccessToken  string            `json:"access_token"`
	BaseURL      string            `json:"base_url"`
	Headers      map[string]string `json:"headers"`
	Disabled     bool              `json:"disabled"`
	RefreshToken string            `json:"refresh_token"`
}

type probeResult struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	AuthIndex     string `json:"auth_index"`
	Provider      string `json:"provider"`
	Status        string `json:"status"`
	StatusMessage string `json:"status_message,omitempty"`
	ProbeHTTP     int    `json:"probe_http,omitempty"`
	ProbeBody     string `json:"probe_body,omitempty"`
	Model         string `json:"model,omitempty"`
	Category      string `json:"category,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Action        string `json:"action,omitempty"`
	Deleted       bool   `json:"deleted,omitempty"`
	Error         string `json:"error,omitempty"`
	CheckedAt     string `json:"checked_at"`
}

type runSummary struct {
	StartedAt      string        `json:"started_at"`
	FinishedAt     string        `json:"finished_at"`
	DurationMS     int64         `json:"duration_ms"`
	Total          int           `json:"total"`
	Healthy        int           `json:"healthy"`
	Unhealthy      int           `json:"unhealthy"`
	Deleted        int           `json:"deleted"`
	Skipped        int           `json:"skipped"`
	Errors         int           `json:"errors"`
	Count402       int           `json:"count_402"`
	Count403       int           `json:"count_403"`
	Count401       int           `json:"count_401"`
	CountRateLimit int           `json:"count_rate_limit"`
	CountOtherBad  int           `json:"count_other_bad"`
	DryRun         bool          `json:"dry_run"`
	Results        []probeResult `json:"results"`
	LastError      string        `json:"last_error,omitempty"`
	TriggeredBy    string        `json:"triggered_by"`
}

type hostHTTPResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte              `json:"Body"`
}

var (
	currentConfig atomic.Value
	workerMu      sync.Mutex
	stopCh        chan struct{}
	running       atomic.Bool
	lastSummary   atomic.Value
	scanInFlight  atomic.Bool
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	currentConfig.Store(defaultConfig())
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	stopWorker()
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		ensureWorker()
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodPluginShutdown:
		stopWorker()
		return okEnvelope(map[string]any{})
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration{
			Resources: []managementResource{{
				Path:        resourcePath,
				Menu:        "XAI Health Janitor",
				Description: "Periodically probes xAI/Grok auths and deletes 402/403/429 or rate-limited accounts.",
			}},
		})
	case pluginabi.MethodManagementHandle:
		return handleManagement(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func defaultConfig() pluginConfig {
	probe := true
	autoDelete := true
	return pluginConfig{
		IntervalSeconds: defaultIntervalSec,
		Model:           defaultModel,
		CLIVersion:      defaultCLIVersion,
		ManagementBase:  defaultMgmtBase,
		ProbeEnabled:    &probe,
		AutoDelete:      &autoDelete,
		DryRun:          false,
		DeleteStatus:    []int{402, 403, 429},
		Providers:       []string{"xai"},
		Concurrency:     3,
	}
}

func configure(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return errUnmarshal
		}
	}
	cfg := defaultConfig()
	if len(req.ConfigYAML) > 0 {
		if errYAML := yaml.Unmarshal(req.ConfigYAML, &cfg); errYAML != nil {
			return fmt.Errorf("decode plugin config: %w", errYAML)
		}
	}
	cfg = normalizeConfig(cfg)
	currentConfig.Store(cfg)
	hostLog("info", fmt.Sprintf("%s configured: interval=%ds model=%s dry_run=%v auto_delete=%v", pluginName, cfg.IntervalSeconds, cfg.Model, cfg.DryRun, boolVal(cfg.AutoDelete, true)))
	return nil
}

func normalizeConfig(cfg pluginConfig) pluginConfig {
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = defaultIntervalSec
	}
	if cfg.IntervalSeconds < 30 {
		cfg.IntervalSeconds = 30
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	cfg.CLIVersion = strings.TrimSpace(cfg.CLIVersion)
	if cfg.CLIVersion == "" {
		cfg.CLIVersion = defaultCLIVersion
	}
	cfg.ManagementBase = strings.TrimRight(strings.TrimSpace(cfg.ManagementBase), "/")
	if cfg.ManagementBase == "" {
		cfg.ManagementBase = defaultMgmtBase
	}
	cfg.ManagementKey = strings.TrimSpace(cfg.ManagementKey)
	if len(cfg.DeleteStatus) == 0 {
		cfg.DeleteStatus = []int{402, 403, 429}
	}
	if len(cfg.Providers) == 0 {
		cfg.Providers = []string{"xai"}
	}
	for i := range cfg.Providers {
		cfg.Providers[i] = strings.ToLower(strings.TrimSpace(cfg.Providers[i]))
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 3
	}
	if cfg.Concurrency > 10 {
		cfg.Concurrency = 10
	}
	if cfg.ProbeEnabled == nil {
		v := true
		cfg.ProbeEnabled = &v
	}
	if cfg.AutoDelete == nil {
		v := true
		cfg.AutoDelete = &v
	}
	return cfg
}

func loadedConfig() pluginConfig {
	if raw := currentConfig.Load(); raw != nil {
		if cfg, ok := raw.(pluginConfig); ok {
			return cfg
		}
	}
	return defaultConfig()
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           "local",
			GitHubRepository: "https://github.com/router-for-me/CLIProxyAPI",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "interval_seconds", Type: pluginapi.ConfigFieldTypeInteger, Description: "Scan interval in seconds (min 30, default 300)."},
				{Name: "model", Type: pluginapi.ConfigFieldTypeString, Description: "Probe model id (default grok-4.5)."},
				{Name: "cli_version", Type: pluginapi.ConfigFieldTypeString, Description: "x-grok-client-version header for cli-chat-proxy."},
				{Name: "management_base", Type: pluginapi.ConfigFieldTypeString, Description: "CPA management base URL used for DELETE auth-files."},
				{Name: "management_key", Type: pluginapi.ConfigFieldTypeString, Description: "CPA remote-management secret key used to delete auth files."},
				{Name: "probe_enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Actually call upstream chat endpoint for each xAI auth."},
				{Name: "auto_delete", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Delete unhealthy auths automatically."},
				{Name: "dry_run", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Only report unhealthy auths, never delete."},
				{Name: "concurrency", Type: pluginapi.ConfigFieldTypeInteger, Description: "Probe concurrency (1-10)."},
			},
		},
		Capabilities: registrationCapabilities{ManagementAPI: true},
	}
}

func ensureWorker() {
	workerMu.Lock()
	defer workerMu.Unlock()
	if stopCh != nil {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
	}
	stopCh = make(chan struct{})
	running.Store(true)
	ch := stopCh
	go workerLoop(ch)
	go func() {
		time.Sleep(3 * time.Second)
		select {
		case <-ch:
			return
		default:
			runScan("startup")
		}
	}()
}

func stopWorker() {
	workerMu.Lock()
	defer workerMu.Unlock()
	if stopCh != nil {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
		stopCh = nil
	}
	running.Store(false)
}

func workerLoop(ch <-chan struct{}) {
	cfg := loadedConfig()
	ticker := time.NewTicker(time.Duration(cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ch:
			return
		case <-ticker.C:
			next := loadedConfig()
			if next.IntervalSeconds != cfg.IntervalSeconds {
				ticker.Reset(time.Duration(next.IntervalSeconds) * time.Second)
				cfg = next
			}
			runScan("timer")
		}
	}
}

func runScan(triggeredBy string) *runSummary {
	if !scanInFlight.CompareAndSwap(false, true) {
		hostLog("info", pluginName+": scan skipped, previous scan still running")
		return nil
	}
	defer scanInFlight.Store(false)

	cfg := loadedConfig()
	started := time.Now()
	summary := &runSummary{
		StartedAt:   started.UTC().Format(time.RFC3339),
		DryRun:      cfg.DryRun,
		TriggeredBy: triggeredBy,
		Results:     make([]probeResult, 0),
	}
	hostLog("info", fmt.Sprintf("%s: scan start (%s)", pluginName, triggeredBy))

	files, errList := callHostAuthList()
	if errList != nil {
		summary.LastError = errList.Error()
		summary.Errors++
		summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		summary.DurationMS = time.Since(started).Milliseconds()
		lastSummary.Store(summary)
		hostLog("error", pluginName+": list auth failed: "+errList.Error())
		return summary
	}

	targets := make([]pluginapi.HostAuthFileEntry, 0)
	for _, f := range files {
		if f.Disabled || f.RuntimeOnly {
			continue
		}
		if !isTargetProvider(f, cfg.Providers) {
			continue
		}
		targets = append(targets, f)
	}
	summary.Total = len(targets)

	type job struct {
		file pluginapi.HostAuthFileEntry
	}
	jobs := make(chan job)
	results := make(chan probeResult, len(targets))
	var wg sync.WaitGroup
	workers := cfg.Concurrency
	if workers > len(targets) && len(targets) > 0 {
		workers = len(targets)
	}
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- inspectAuth(cfg, j.file)
			}
		}()
	}
	go func() {
		for _, f := range targets {
			jobs <- job{file: f}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	for item := range results {
		item.Category = categoryOf(item)
		summary.Results = append(summary.Results, item)
		switch item.Category {
		case "healthy":
			summary.Healthy++
		case "http_402":
			summary.Count402++
			summary.Unhealthy++
		case "http_403":
			summary.Count403++
			summary.Unhealthy++
		case "http_401":
			summary.Count401++
			summary.Unhealthy++
		case "rate_limit":
			summary.CountRateLimit++
			summary.Unhealthy++
		case "error":
			summary.Errors++
		default:
			if item.Action == "keep" {
				summary.Healthy++
			} else if item.Action == "deleted" || item.Action == "would_delete" || item.Action == "delete_failed" {
				summary.CountOtherBad++
				summary.Unhealthy++
			} else {
				summary.Skipped++
			}
		}
		if item.Deleted {
			summary.Deleted++
		}
	}

	summary.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	summary.DurationMS = time.Since(started).Milliseconds()
	lastSummary.Store(summary)
	hostLog("info", fmt.Sprintf("%s: scan done total=%d healthy=%d unhealthy=%d deleted=%d errors=%d dry_run=%v",
		pluginName, summary.Total, summary.Healthy, summary.Unhealthy, summary.Deleted, summary.Errors, summary.DryRun))
	return summary
}

func isTargetProvider(f pluginapi.HostAuthFileEntry, providers []string) bool {
	p := strings.ToLower(strings.TrimSpace(f.Provider))
	if p == "" {
		p = strings.ToLower(strings.TrimSpace(f.Type))
	}
	name := strings.ToLower(f.Name)
	for _, want := range providers {
		if p == want || strings.HasPrefix(name, want+"-") {
			return true
		}
	}
	return false
}

func inspectAuth(cfg pluginConfig, file pluginapi.HostAuthFileEntry) probeResult {
	result := probeResult{
		Name:          file.Name,
		Email:         file.Email,
		AuthIndex:     file.AuthIndex,
		Provider:      firstNonEmpty(file.Provider, file.Type),
		Status:        file.Status,
		StatusMessage: file.StatusMessage,
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	// Fast path: management status already shows hard failure text.
	if reason := classifyText(file.StatusMessage); reason != "" {
		result.Reason = reason
		return maybeDelete(cfg, result)
	}
	if strings.EqualFold(file.Status, "error") && strings.TrimSpace(file.StatusMessage) != "" {
		if reason := classifyText(file.StatusMessage); reason != "" {
			result.Reason = reason
			return maybeDelete(cfg, result)
		}
	}

	if !boolVal(cfg.ProbeEnabled, true) {
		result.Action = "keep"
		result.Reason = "probe_disabled"
		return result
	}

	authJSON, name, errGet := callHostAuthGet(file.AuthIndex)
	if errGet != nil {
		result.Error = "host.auth.get: " + errGet.Error()
		return result
	}
	if name != "" {
		result.Name = name
	}
	var auth xaiAuthFile
	if errUnmarshal := json.Unmarshal(authJSON, &auth); errUnmarshal != nil {
		result.Error = "decode auth json: " + errUnmarshal.Error()
		return result
	}
	if auth.Email != "" {
		result.Email = auth.Email
	}
	token := strings.TrimSpace(auth.AccessToken)
	if token == "" {
		result.Reason = "missing_access_token"
		return maybeDelete(cfg, result)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(auth.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	cliVersion := cfg.CLIVersion
	if auth.Headers != nil {
		if v := strings.TrimSpace(auth.Headers["x-grok-client-version"]); v != "" {
			cliVersion = v
		}
	}

	body, _ := json.Marshal(map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 4,
	})
	headers := map[string][]string{
		"Authorization":          { "Bearer " + token },
		"Content-Type":           {"application/json"},
		"Accept":                 {"application/json"},
		"x-grok-client-version":  {cliVersion},
		"User-Agent":             {"xai-health-janitor/" + pluginVersion},
	}
	resp, errHTTP := callHostHTTP("POST", baseURL+"/chat/completions", headers, body)
	if errHTTP != nil {
		// Network failures should not delete accounts.
		result.Error = "probe: " + errHTTP.Error()
		result.Action = "keep"
		return result
	}
	result.ProbeHTTP = resp.StatusCode
	result.ProbeBody = trimBody(resp.Body, 300)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var parsed map[string]any
		_ = json.Unmarshal(resp.Body, &parsed)
		if model, ok := parsed["model"].(string); ok {
			result.Model = model
		}
		result.Action = "keep"
		result.Reason = "healthy"
		return result
	}

	reason := classifyStatus(resp.StatusCode, string(resp.Body), cfg.DeleteStatus)
	if reason == "" {
		result.Action = "keep"
		result.Reason = fmt.Sprintf("non_fatal_http_%d", resp.StatusCode)
		return result
	}
	result.Reason = reason
	return maybeDelete(cfg, result)
}

func maybeDelete(cfg pluginConfig, result probeResult) probeResult {
	if result.Reason == "" {
		result.Action = "keep"
		return result
	}
	if !boolVal(cfg.AutoDelete, true) || cfg.DryRun {
		result.Action = "would_delete"
		return result
	}
	if strings.TrimSpace(cfg.ManagementKey) == "" {
		result.Action = "would_delete"
		result.Error = "management_key is empty; cannot delete"
		return result
	}
	name := strings.TrimSpace(result.Name)
	if name == "" {
		result.Action = "would_delete"
		result.Error = "auth file name is empty"
		return result
	}
	if errDelete := deleteAuthFile(cfg, name); errDelete != nil {
		result.Action = "delete_failed"
		result.Error = errDelete.Error()
		return result
	}
	result.Action = "deleted"
	result.Deleted = true
	hostLog("warn", fmt.Sprintf("%s: deleted unhealthy auth %s (%s) reason=%s", pluginName, name, result.Email, result.Reason))
	return result
}

func deleteAuthFile(cfg pluginConfig, name string) error {
	endpoint := cfg.ManagementBase + "/v0/management/auth-files?name=" + url.QueryEscape(name)
	headers := map[string][]string{
		"Authorization": {"Bearer " + cfg.ManagementKey},
		"Accept":        {"application/json"},
	}
	resp, errHTTP := callHostHTTP("DELETE", endpoint, headers, nil)
	if errHTTP != nil {
		return errHTTP
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("delete HTTP %d: %s", resp.StatusCode, trimBody(resp.Body, 200))
}

func classifyStatus(code int, body string, deleteCodes []int) string {
	for _, want := range deleteCodes {
		if code == want {
			if textReason := classifyText(body); textReason != "" {
				return fmt.Sprintf("http_%d:%s", code, textReason)
			}
			return fmt.Sprintf("http_%d", code)
		}
	}
	if textReason := classifyText(body); textReason != "" {
		// Text-based hard failures even if status is unexpected.
		if code == 402 || code == 403 || code == 429 {
			return fmt.Sprintf("http_%d:%s", code, textReason)
		}
	}
	return ""
}

func classifyText(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "permission-denied"), strings.Contains(lower, "access to the chat endpoint is denied"):
		return "permission_denied"
	case strings.Contains(lower, "spending-limit"), strings.Contains(lower, "run out of credits"), strings.Contains(lower, "personal-team-blocked"):
		return "spending_limit"
	case strings.Contains(lower, "rate limit"), strings.Contains(lower, "rate_limit"), strings.Contains(lower, "too many requests"), strings.Contains(lower, "resource_exhausted"):
		return "rate_limited"
	case strings.Contains(lower, "upgrade required"), strings.Contains(lower, "cli version"):
		// Version issue is config problem, not account death; keep account.
		return ""
	case strings.Contains(lower, "unauthenticated"), strings.Contains(lower, "invalid token"), strings.Contains(lower, "token expired"):
		return "auth_invalid"
	default:
		return ""
	}
}

func categoryOf(item probeResult) string {
	if item.Error != "" && item.Action == "keep" {
		return "error"
	}
	if item.Reason == "healthy" || (item.Action == "keep" && item.ProbeHTTP >= 200 && item.ProbeHTTP < 300) {
		return "healthy"
	}
	blob := strings.ToLower(item.Reason + " " + item.ProbeBody + " " + item.StatusMessage)
	switch {
	case item.ProbeHTTP == 402 || strings.Contains(blob, "spending") || strings.Contains(blob, "402"):
		return "http_402"
	case item.ProbeHTTP == 403 || strings.Contains(blob, "permission_denied") || strings.Contains(blob, "403"):
		return "http_403"
	case item.ProbeHTTP == 401 || strings.Contains(blob, "auth_invalid") || strings.Contains(blob, "unauthenticated") || strings.Contains(blob, "401"):
		return "http_401"
	case item.ProbeHTTP == 429 || strings.Contains(blob, "rate_limit") || strings.Contains(blob, "rate limit") || strings.Contains(blob, "429"):
		return "rate_limit"
	case item.Action == "deleted" || item.Action == "would_delete" || item.Action == "delete_failed":
		return "other_bad"
	case item.Error != "":
		return "error"
	default:
		return "other"
	}
}

func handleManagement(raw []byte) ([]byte, error) {
	var req managementRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return nil, fmt.Errorf("decode management request: %w", errUnmarshal)
		}
	}
	op := "status"
	if req.Query != nil {
		if v := strings.TrimSpace(req.Query.Get("op")); v != "" {
			op = strings.ToLower(v)
		}
	}
	if len(req.Body) > 0 {
		var body struct {
			Op string `json:"op"`
		}
		if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal == nil && strings.TrimSpace(body.Op) != "" {
			op = strings.ToLower(strings.TrimSpace(body.Op))
		}
	}

	switch op {
	case "scan", "run":
		summary := runScan("manual")
		if summary == nil {
			summary = &runSummary{LastError: "scan already running", TriggeredBy: "manual"}
		}
		if wantsJSON(req) {
			return okEnvelope(jsonResponse(http.StatusOK, summary))
		}
		return okEnvelope(htmlResponse(http.StatusOK, renderPage(loadedConfig(), summary, "")))
	case "save_settings", "settings":
		msg, errSave := applySettings(req)
		cfg := loadedConfig()
		summary := loadSummary()
		if wantsJSON(req) {
			payload := map[string]any{"config": sanitizeConfig(cfg), "summary": summary, "message": msg}
			if errSave != nil {
				payload["error"] = errSave.Error()
				return okEnvelope(jsonResponse(http.StatusBadRequest, payload))
			}
			return okEnvelope(jsonResponse(http.StatusOK, payload))
		}
		errText := ""
		if errSave != nil {
			errText = errSave.Error()
		} else if msg != "" {
			errText = msg
		}
		return okEnvelope(htmlResponse(http.StatusOK, renderPage(cfg, summary, errText)))
	case "config":
		cfg := loadedConfig()
		if wantsJSON(req) {
			return okEnvelope(jsonResponse(http.StatusOK, sanitizeConfig(cfg)))
		}
		return okEnvelope(htmlResponse(http.StatusOK, renderPage(cfg, loadSummary(), "")))
	default:
		cfg := loadedConfig()
		summary := loadSummary()
		if wantsJSON(req) {
			return okEnvelope(jsonResponse(http.StatusOK, map[string]any{
				"config":  sanitizeConfig(cfg),
				"summary": summary,
			}))
		}
		return okEnvelope(htmlResponse(http.StatusOK, renderPage(cfg, summary, "")))
	}
}

func applySettings(req managementRequest) (string, error) {
	cfg := loadedConfig()
	changed := false

	readInt := func(raw string) (int, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return 0, false
		}
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
			return 0, false
		}
		return n, true
	}
	readBool := func(raw string) (bool, bool) {
		// form may send "false,true" when hidden+checkbox both present
		parts := strings.Split(raw, ",")
		raw = strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		switch raw {
		case "1", "true", "on", "yes":
			return true, true
		case "0", "false", "off", "no":
			return false, true
		default:
			return false, false
		}
	}

	if req.Query != nil {
		if n, ok := readInt(req.Query.Get("interval_seconds")); ok {
			cfg.IntervalSeconds = n
			changed = true
		}
		if n, ok := readInt(req.Query.Get("concurrency")); ok {
			cfg.Concurrency = n
			changed = true
		}
		if v := strings.TrimSpace(req.Query.Get("model")); v != "" {
			cfg.Model = v
			changed = true
		}
		if v := strings.TrimSpace(req.Query.Get("cli_version")); v != "" {
			cfg.CLIVersion = v
			changed = true
		}
		if b, ok := readBool(req.Query.Get("auto_delete")); ok {
			cfg.AutoDelete = &b
			changed = true
		}
		if b, ok := readBool(req.Query.Get("dry_run")); ok {
			cfg.DryRun = b
			changed = true
		}
		if b, ok := readBool(req.Query.Get("probe_enabled")); ok {
			cfg.ProbeEnabled = &b
			changed = true
		}
	}

	if len(req.Body) > 0 {
		// support application/x-www-form-urlencoded and JSON
		ct := ""
		// body may be form
		if values, errParse := url.ParseQuery(string(req.Body)); errParse == nil && len(values) > 0 && !json.Valid(req.Body) {
			last := func(key string) string {
				arr := values[key]
				if len(arr) == 0 {
					return ""
				}
				return arr[len(arr)-1]
			}
			if n, ok := readInt(last("interval_seconds")); ok {
				cfg.IntervalSeconds = n
				changed = true
			}
			if n, ok := readInt(last("concurrency")); ok {
				cfg.Concurrency = n
				changed = true
			}
			if v := strings.TrimSpace(last("model")); v != "" {
				cfg.Model = v
				changed = true
			}
			if v := strings.TrimSpace(last("cli_version")); v != "" {
				cfg.CLIVersion = v
				changed = true
			}
			if b, ok := readBool(last("auto_delete")); ok {
				cfg.AutoDelete = &b
				changed = true
			}
			if b, ok := readBool(last("dry_run")); ok {
				cfg.DryRun = b
				changed = true
			}
			if b, ok := readBool(last("probe_enabled")); ok {
				cfg.ProbeEnabled = &b
				changed = true
			}
			_ = ct
		} else {
			var body map[string]any
			if errUnmarshal := json.Unmarshal(req.Body, &body); errUnmarshal == nil {
				if v, ok := body["interval_seconds"]; ok {
					switch n := v.(type) {
					case float64:
						cfg.IntervalSeconds = int(n)
						changed = true
					case string:
						if n2, ok2 := readInt(n); ok2 {
							cfg.IntervalSeconds = n2
							changed = true
						}
					}
				}
				if v, ok := body["concurrency"]; ok {
					if n, ok2 := v.(float64); ok2 {
						cfg.Concurrency = int(n)
						changed = true
					}
				}
				if v, ok := body["model"].(string); ok && strings.TrimSpace(v) != "" {
					cfg.Model = strings.TrimSpace(v)
					changed = true
				}
				if v, ok := body["cli_version"].(string); ok && strings.TrimSpace(v) != "" {
					cfg.CLIVersion = strings.TrimSpace(v)
					changed = true
				}
				if v, ok := body["auto_delete"].(bool); ok {
					cfg.AutoDelete = &v
					changed = true
				}
				if v, ok := body["dry_run"].(bool); ok {
					cfg.DryRun = v
					changed = true
				}
				if v, ok := body["probe_enabled"].(bool); ok {
					cfg.ProbeEnabled = &v
					changed = true
				}
			}
		}
	}

	if !changed {
		return "未检测到配置变更", nil
	}
	cfg = normalizeConfig(cfg)
	currentConfig.Store(cfg)
	ensureWorker()
	persistMsg := persistPluginConfig(cfg)
	hostLog("info", fmt.Sprintf("%s settings updated: interval=%ds auto_delete=%v dry_run=%v", pluginName, cfg.IntervalSeconds, boolVal(cfg.AutoDelete, true), cfg.DryRun))
	if persistMsg != "" {
		return "设置已生效（运行时）。" + persistMsg, nil
	}
	return "设置已保存并立即生效", nil
}

func persistPluginConfig(cfg pluginConfig) string {
	// Best-effort: persist via CPA management plugin config API.
	if strings.TrimSpace(cfg.ManagementKey) == "" {
		return "未配置 management_key，仅运行时生效，重启后可能丢失。"
	}
	payload, _ := json.Marshal(map[string]any{
		"enabled":             true,
		"priority":            1,
		"interval_seconds":    cfg.IntervalSeconds,
		"model":               cfg.Model,
		"cli_version":         cfg.CLIVersion,
		"management_base":     cfg.ManagementBase,
		"management_key":      cfg.ManagementKey,
		"probe_enabled":       boolVal(cfg.ProbeEnabled, true),
		"auto_delete":         boolVal(cfg.AutoDelete, true),
		"dry_run":             cfg.DryRun,
		"concurrency":         cfg.Concurrency,
		"delete_status_codes": cfg.DeleteStatus,
		"providers":           cfg.Providers,
	})
	endpoint := strings.TrimRight(cfg.ManagementBase, "/") + "/v0/management/plugins/" + pluginName + "/config"
	headers := map[string][]string{
		"Authorization": {"Bearer " + cfg.ManagementKey},
		"Content-Type":  {"application/json"},
		"Accept":        {"application/json"},
	}
	resp, errHTTP := callHostHTTP("PUT", endpoint, headers, payload)
	if errHTTP != nil {
		return "持久化到 CPA 配置失败: " + errHTTP.Error()
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "已同步写入 CPA 配置。"
	}
	// try PATCH
	resp2, err2 := callHostHTTP("PATCH", endpoint, headers, payload)
	if err2 == nil && resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
		return "已同步写入 CPA 配置。"
	}
	return fmt.Sprintf("持久化返回 HTTP %d: %s", resp.StatusCode, trimBody(resp.Body, 120))
}

func wantsJSON(req managementRequest) bool {
	if req.Query != nil && strings.EqualFold(req.Query.Get("format"), "json") {
		return true
	}
	return false
}

func sanitizeConfig(cfg pluginConfig) map[string]any {
	key := cfg.ManagementKey
	if key != "" {
		if len(key) <= 6 {
			key = "***"
		} else {
			key = key[:2] + "***" + key[len(key)-2:]
		}
	}
	return map[string]any{
		"interval_seconds":    cfg.IntervalSeconds,
		"model":               cfg.Model,
		"cli_version":         cfg.CLIVersion,
		"management_base":     cfg.ManagementBase,
		"management_key":      key,
		"probe_enabled":       boolVal(cfg.ProbeEnabled, true),
		"auto_delete":         boolVal(cfg.AutoDelete, true),
		"dry_run":             cfg.DryRun,
		"delete_status_codes": cfg.DeleteStatus,
		"providers":           cfg.Providers,
		"concurrency":         cfg.Concurrency,
	}
}

func loadSummary() *runSummary {
	if raw := lastSummary.Load(); raw != nil {
		if s, ok := raw.(*runSummary); ok {
			return s
		}
	}
	return nil
}

func renderPage(cfg pluginConfig, summary *runSummary, notice string) []byte {
	total, healthy, unhealthy, deleted := 0, 0, 0, 0
	c402, c403, c401, c429, cErr := 0, 0, 0, 0, 0
	triggered, finished, duration := "-", "-", int64(0)
	if summary != nil {
		total = summary.Total
		healthy = summary.Healthy
		unhealthy = summary.Unhealthy
		deleted = summary.Deleted
		c402 = summary.Count402
		c403 = summary.Count403
		c401 = summary.Count401
		c429 = summary.CountRateLimit
		cErr = summary.Errors
		if summary.TriggeredBy != "" {
			triggered = summary.TriggeredBy
		}
		if summary.FinishedAt != "" {
			finished = summary.FinishedAt
		}
		duration = summary.DurationMS
	}

	var out bytes.Buffer
	out.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	out.WriteString(`<title>XAI Health Janitor</title><style>
:root{--bg:#f4f6fb;--card:#fff;--text:#111827;--muted:#6b7280;--line:#e5e7eb;--ok:#059669;--okbg:#ecfdf5;--bad:#dc2626;--badbg:#fef2f2;--warn:#d97706;--warnbg:#fffbeb;--info:#2563eb;--infobg:#eff6ff;--shadow:0 8px 24px rgba(15,23,42,.06)}
*{box-sizing:border-box}body{margin:0;font-family:Inter,ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;background:linear-gradient(180deg,#eef2ff 0%,var(--bg) 220px);color:var(--text)}
.wrap{max-width:1200px;margin:0 auto;padding:28px 20px 48px}header{display:flex;justify-content:space-between;gap:16px;align-items:flex-start;margin-bottom:20px}
h1{margin:0 0 6px;font-size:28px;letter-spacing:-.02em}.sub{color:var(--muted);margin:0}
.actions{display:flex;gap:10px;flex-wrap:wrap}.btn{display:inline-flex;align-items:center;gap:8px;border:1px solid var(--line);background:#fff;color:var(--text);border-radius:12px;padding:10px 14px;text-decoration:none;font-weight:600;box-shadow:var(--shadow);cursor:pointer}
.btn-primary{background:#111827;color:#fff;border-color:#111827}.btn-danger{background:var(--bad);color:#fff;border-color:var(--bad)}
.grid{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:12px;margin:18px 0 22px}
.card{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:16px;box-shadow:var(--shadow)}.card .label{color:var(--muted);font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.04em}.card .value{font-size:30px;font-weight:760;margin-top:8px;line-height:1.1}.card .hint{margin-top:6px;color:var(--muted);font-size:12px}
.ok .value{color:var(--ok)}.bad .value{color:var(--bad)}.warn .value{color:var(--warn)}.info .value{color:var(--info)}
.panel{background:var(--card);border:1px solid var(--line);border-radius:18px;box-shadow:var(--shadow);padding:18px;margin-bottom:18px}
.panel h2{margin:0 0 14px;font-size:18px}.meta{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:14px}
.pill{display:inline-flex;align-items:center;gap:6px;border-radius:999px;padding:6px 10px;background:#f3f4f6;color:#374151;font-size:12px;font-weight:600}
.form{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.field label{display:block;font-size:12px;font-weight:700;color:var(--muted);margin-bottom:6px}
.field input[type=number],.field input[type=text],.field select{width:100%;border:1px solid var(--line);border-radius:12px;padding:10px 12px;font:inherit;background:#fff}
.checks{display:flex;gap:16px;align-items:center;flex-wrap:wrap;padding-top:24px}.checks label{display:flex;gap:8px;align-items:center;font-weight:600}
.notice{margin:0 0 16px;padding:12px 14px;border-radius:12px;background:var(--infobg);color:#1d4ed8;border:1px solid #bfdbfe}
.notice.bad{background:var(--badbg);color:#991b1b;border-color:#fecaca}
.table-wrap{overflow:auto}table{width:100%;border-collapse:collapse;min-width:900px}th,td{padding:12px 10px;border-bottom:1px solid var(--line);text-align:left;font-size:13px;vertical-align:top}th{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.04em;background:#fafafa;position:sticky;top:0}
.badge{display:inline-flex;border-radius:999px;padding:4px 8px;font-size:12px;font-weight:700}.b-ok{background:var(--okbg);color:var(--ok)}.b-bad{background:var(--badbg);color:var(--bad)}.b-warn{background:var(--warnbg);color:var(--warn)}.b-muted{background:#f3f4f6;color:#4b5563}
.muted{color:var(--muted)}code{background:#f3f4f6;border-radius:6px;padding:.1rem .35rem}
@media (max-width:1000px){.grid{grid-template-columns:repeat(3,minmax(0,1fr))}.form{grid-template-columns:1fr}}
@media (max-width:640px){.grid{grid-template-columns:repeat(2,minmax(0,1fr))}header{flex-direction:column}}
</style></head><body><div class="wrap">`)

	out.WriteString(`<header><div><h1>XAI Health Janitor</h1><p class="sub">定时探测 Grok/xAI 账号；402 / 401 / 403 / 限流自动删除</p></div><div class="actions">`)
	out.WriteString(`<a class="btn btn-primary" href="?op=scan">立即扫描</a>`)
	out.WriteString(`<a class="btn" href="?op=status">刷新</a>`)
	out.WriteString(`<a class="btn" href="?op=status&format=json">JSON</a></div></header>`)

	if strings.TrimSpace(notice) != "" {
		cls := "notice"
		if strings.Contains(strings.ToLower(notice), "fail") || strings.Contains(notice, "失败") || strings.Contains(notice, "错误") {
			cls += " bad"
		}
		out.WriteString(`<div class="` + cls + `">` + html.EscapeString(notice) + `</div>`)
	}

	// stats cards
	out.WriteString(`<div class="grid">`)
	writeStatCard(&out, "info", "总账号", fmt.Sprintf("%d", total), "当前池内 xAI 账号")
	writeStatCard(&out, "ok", "正常", fmt.Sprintf("%d", healthy), "探测成功可调用")
	writeStatCard(&out, "bad", "异常", fmt.Sprintf("%d", unhealthy), "需关注/已处理")
	writeStatCard(&out, "warn", "402 额度", fmt.Sprintf("%d", c402), "spending-limit")
	writeStatCard(&out, "bad", "403 拒绝", fmt.Sprintf("%d", c403), "permission-denied")
	writeStatCard(&out, "warn", "401/限流", fmt.Sprintf("%d", c401+c429), fmt.Sprintf("401=%d · 429=%d", c401, c429))
	out.WriteString(`</div>`)

	// settings panel
	out.WriteString(`<section class="panel"><h2>扫描设置</h2>`)
	out.WriteString(`<form class="form" method="post" action="?op=save_settings">`)
	out.WriteString(`<div class="field"><label>轮询间隔（秒）</label><input type="number" name="interval_seconds" min="30" step="30" value="` + fmt.Sprintf("%d", cfg.IntervalSeconds) + `"></div>`)
	out.WriteString(`<div class="field"><label>并发数</label><input type="number" name="concurrency" min="1" max="10" value="` + fmt.Sprintf("%d", cfg.Concurrency) + `"></div>`)
	out.WriteString(`<div class="field"><label>探测模型</label><input type="text" name="model" value="` + html.EscapeString(cfg.Model) + `"></div>`)
	out.WriteString(`<div class="field"><label>CLI Version 头</label><input type="text" name="cli_version" value="` + html.EscapeString(cfg.CLIVersion) + `"></div>`)
	out.WriteString(`<div class="checks">`)
	out.WriteString(checkBox("probe_enabled", "启用探测", boolVal(cfg.ProbeEnabled, true)))
	out.WriteString(checkBox("auto_delete", "自动删除异常号", boolVal(cfg.AutoDelete, true)))
	out.WriteString(checkBox("dry_run", "仅演练(不删除)", cfg.DryRun))
	out.WriteString(`<button class="btn btn-primary" type="submit">保存设置</button>`)
	out.WriteString(`</div></form>`)
	out.WriteString(`<p class="muted" style="margin:12px 0 0">自动删除范围：HTTP 402 / 403 / 429，以及 permission-denied / spending-limit / rate-limit / auth invalid。当前已删除：<strong>` + fmt.Sprintf("%d", deleted) + `</strong></p>`)
	out.WriteString(`</section>`)

	// last scan panel
	out.WriteString(`<section class="panel"><h2>最近一次扫描</h2><div class="meta">`)
	out.WriteString(`<span class="pill">触发：` + html.EscapeString(triggered) + `</span>`)
	out.WriteString(`<span class="pill">完成：` + html.EscapeString(finished) + `</span>`)
	out.WriteString(`<span class="pill">耗时：` + fmt.Sprintf("%dms", duration) + `</span>`)
	out.WriteString(`<span class="pill">探测错误：` + fmt.Sprintf("%d", cErr) + `</span>`)
	out.WriteString(`</div>`)

	if summary == nil {
		out.WriteString(`<p class="muted">还没有扫描结果。点击「立即扫描」开始。</p>`)
	} else {
		if summary.LastError != "" {
			out.WriteString(`<div class="notice bad">` + html.EscapeString(summary.LastError) + `</div>`)
		}
		out.WriteString(`<div class="table-wrap"><table><thead><tr><th>账号文件</th><th>邮箱</th><th>HTTP</th><th>分类</th><th>原因</th><th>动作</th><th>错误</th></tr></thead><tbody>`)
		// show bad first
		ordered := make([]probeResult, 0, len(summary.Results))
		for _, item := range summary.Results {
			if item.Category != "healthy" {
				ordered = append(ordered, item)
			}
		}
		for _, item := range summary.Results {
			if item.Category == "healthy" {
				ordered = append(ordered, item)
			}
		}
		for _, item := range ordered {
			cat := item.Category
			if cat == "" {
				cat = categoryOf(item)
			}
			badge := "b-muted"
			switch cat {
			case "healthy":
				badge = "b-ok"
			case "http_402", "http_403", "http_401", "rate_limit", "other_bad":
				badge = "b-bad"
			case "error":
				badge = "b-warn"
			}
			actionBadge := "b-muted"
			if item.Deleted || item.Action == "deleted" {
				actionBadge = "b-bad"
			} else if item.Action == "keep" {
				actionBadge = "b-ok"
			} else if item.Action == "would_delete" {
				actionBadge = "b-warn"
			}
			out.WriteString("<tr>")
			out.WriteString("<td><code>" + html.EscapeString(item.Name) + "</code></td>")
			out.WriteString("<td>" + html.EscapeString(item.Email) + "</td>")
			out.WriteString(fmt.Sprintf("<td>%d</td>", item.ProbeHTTP))
			out.WriteString(`<td><span class="badge ` + badge + `">` + html.EscapeString(displayCategory(cat)) + `</span></td>`)
			out.WriteString("<td>" + html.EscapeString(item.Reason) + "</td>")
			out.WriteString(`<td><span class="badge ` + actionBadge + `">` + html.EscapeString(item.Action) + `</span></td>`)
			out.WriteString("<td class=\"muted\">" + html.EscapeString(item.Error) + "</td>")
			out.WriteString("</tr>")
		}
		out.WriteString(`</tbody></table></div>`)
	}
	out.WriteString(`</section></div></body></html>`)
	return out.Bytes()
}

func writeStatCard(out *bytes.Buffer, tone, label, value, hint string) {
	out.WriteString(`<div class="card ` + tone + `"><div class="label">` + html.EscapeString(label) + `</div><div class="value">` + html.EscapeString(value) + `</div><div class="hint">` + html.EscapeString(hint) + `</div></div>`)
}

func checkBox(name, label string, checked bool) string {
	c := ""
	if checked {
		c = " checked"
	}
	return `<label><input type="hidden" name="` + name + `" value="false"><input type="checkbox" name="` + name + `" value="true"` + c + `> ` + html.EscapeString(label) + `</label>`
}

func displayCategory(cat string) string {
	switch cat {
	case "healthy":
		return "正常"
	case "http_402":
		return "402 额度"
	case "http_403":
		return "403 拒绝"
	case "http_401":
		return "401 鉴权"
	case "rate_limit":
		return "限流 429"
	case "other_bad":
		return "其他异常"
	case "error":
		return "探测错误"
	default:
		return cat
	}
}

func htmlResponse(statusCode int, body []byte) managementResponse {
	return managementResponse{
		StatusCode: statusCode,
		Headers:    http.Header{"content-type": []string{resourceContentType}},
		Body:       body,
	}
}

func jsonResponse(statusCode int, v any) managementResponse {
	raw, _ := json.MarshalIndent(v, "", "  ")
	return managementResponse{
		StatusCode: statusCode,
		Headers:    http.Header{"content-type": []string{"application/json; charset=utf-8"}},
		Body:       raw,
	}
}

func callHostAuthList() ([]pluginapi.HostAuthFileEntry, error) {
	result, errCall := callHost(pluginabi.MethodHostAuthList, map[string]any{})
	if errCall != nil {
		return nil, errCall
	}
	var resp authListResponse
	if errUnmarshal := json.Unmarshal(result, &resp); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host.auth.list: %w", errUnmarshal)
	}
	return resp.Files, nil
}

func callHostAuthGet(authIndex string) (json.RawMessage, string, error) {
	result, errCall := callHost(pluginabi.MethodHostAuthGet, pluginapi.HostAuthGetRequest{AuthIndex: authIndex})
	if errCall != nil {
		return nil, "", errCall
	}
	var resp pluginapi.HostAuthGetResponse
	if errUnmarshal := json.Unmarshal(result, &resp); errUnmarshal != nil {
		return nil, "", fmt.Errorf("decode host.auth.get: %w", errUnmarshal)
	}
	return resp.JSON, resp.Name, nil
}

func callHostHTTP(method, rawURL string, headers map[string][]string, body []byte) (hostHTTPResponse, error) {
	payload := map[string]any{
		"method":  method,
		"url":     rawURL,
		"headers": headers,
		"body":    body,
	}
	result, errCall := callHost(pluginabi.MethodHostHTTPDo, payload)
	if errCall != nil {
		return hostHTTPResponse{}, errCall
	}
	var resp hostHTTPResponse
	if errUnmarshal := json.Unmarshal(result, &resp); errUnmarshal != nil {
		// Some hosts may emit lower-case json field names.
		var alt struct {
			StatusCode int                 `json:"status_code"`
			Headers    map[string][]string `json:"headers"`
			Body       []byte              `json:"body"`
		}
		if errAlt := json.Unmarshal(result, &alt); errAlt != nil {
			return hostHTTPResponse{}, fmt.Errorf("decode host.http.do: %w", errUnmarshal)
		}
		return hostHTTPResponse{StatusCode: alt.StatusCode, Headers: alt.Headers, Body: alt.Body}, nil
	}
	return resp, nil
}

func callHost(method string, payload any) (json.RawMessage, error) {
	rawPayload, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal host callback payload %s: %w", method, errMarshal)
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload == nil {
			return nil, fmt.Errorf("allocate host callback payload %s", method)
		}
		defer C.free(cPayload)
		requestPtr = (*C.uint8_t)(cPayload)
	}
	callCode := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	var rawResponse []byte
	if response.ptr != nil && response.len > 0 {
		rawResponse = C.GoBytes(response.ptr, C.int(response.len))
	}
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, int(callCode))
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(rawResponse, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host callback envelope %s: %w", method, errUnmarshal)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if callCode != 0 {
		return nil, fmt.Errorf("host callback %s returned code=%d", method, int(callCode))
	}
	return append(json.RawMessage(nil), env.Result...), nil
}

func hostLog(level, message string) {
	_, _ = callHost(pluginabi.MethodHostLog, map[string]any{
		"level":   level,
		"message": message,
	})
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

func prettyJSON(v any) string {
	raw, errMarshal := json.MarshalIndent(v, "", "  ")
	if errMarshal != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(raw)
}

func trimBody(raw []byte, limit int) string {
	s := strings.TrimSpace(string(raw))
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func boolVal(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
