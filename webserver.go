package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

//go:embed web/templates/*
var templateFS embed.FS

// WebServer handles the HTTP server for monitoring
type WebServer struct {
	service  *CakeAutoRTTService
	config   *Config
	clients  map[*websocket.Conn]bool
	clientMu sync.RWMutex
	upgrader websocket.Upgrader
	logChan  chan LogMessage
}

// LogMessage represents a log entry for the web interface
type LogMessage struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// QdiscStats represents traffic control qdisc statistics
type QdiscStats struct {
	Interface string `json:"interface"`
	Qdisc     string `json:"qdisc"`
	Stats     string `json:"stats"`
	RTT       string `json:"rtt"`
}

// WebSystemStatus represents the current system status for web interface
type WebSystemStatus struct {
	Timestamp     string       `json:"timestamp"`
	ServiceStatus string       `json:"service_status"`
	ActiveHosts   int          `json:"active_hosts"`
	CurrentRTT    string       `json:"current_rtt"`
	QdiscStats    []QdiscStats `json:"qdisc_stats"`
	RecentLogs    []LogMessage `json:"recent_logs"`
}

// NewWebServer creates a new web server instance
func NewWebServer(service *CakeAutoRTTService, config *Config) *WebServer {
	return &WebServer{
		service: service,
		config:  config,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for simplicity
			},
		},
		logChan: make(chan LogMessage, 100),
	}
}

// Start starts the web server
func (ws *WebServer) Start() error {
	if !ws.config.WebEnabled {
		return nil
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Load templates with the following behavior:
	// 1. If `web/templates/*` exists on disk, prefer parsing those so users can override templates.
	// 2. Always register embedded templates as a fallback for any missing templates (this
	//    ensures the binary works even if the on-disk HTML is deleted).
	var tmpl *template.Template

	// Helper: register embedded templates (by base name) into tmpl if missing
	registerEmbeddedAsBase := func(t *template.Template) error {
		entries, err := templateFS.ReadDir("web/templates")
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			full := filepath.ToSlash(filepath.Join("web/templates", e.Name()))
			// if the template (by base name) is missing, add it from the embedded FS
			if t.Lookup(e.Name()) == nil {
				b, err := templateFS.ReadFile(full)
				if err != nil {
					return err
				}
				if _, err := t.New(e.Name()).Parse(string(b)); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Prefer embedded templates (compiled into the binary) so a shipped
	// release binary will render the UI without requiring on-disk HTML.
	// Allow on-disk templates to override embedded templates when present.
	var tmplFound bool
	var embeddedLoaded bool

	// Try to parse embedded templates first
	var embeddedT *template.Template
	if t, err := template.New("").ParseFS(templateFS, "web/templates/*"); err == nil {
		embeddedT = t
		embeddedLoaded = true
		logMessage("DEBUG", "Embedded templates parsed successfully")
	} else {
		logMessage("DEBUG", "No embedded templates available or parse failed")
	}

	// Disk candidates (relative, distro, OpenWrt)
	diskCandidates := []string{
		"web/templates/index.html",
		"/usr/share/cake-autortt/web/templates/index.html",
		"/etc/cake-autortt/web/templates/index.html", // OpenWrt-friendly location
	}

	// Diagnostic: log which candidate paths exist (helps debugging on devices)
	var candStatus []string
	for _, cand := range diskCandidates {
		if _, err := os.Stat(cand); err == nil {
			candStatus = append(candStatus, fmt.Sprintf("%s=exists", cand))
		} else {
			candStatus = append(candStatus, fmt.Sprintf("%s=missing", cand))
		}
	}
	logMessage("DEBUG", fmt.Sprintf("Template candidates: %s", strings.Join(candStatus, ", ")))

	// If a disk template dir exists, prefer it (it will override embedded)
	for _, cand := range diskCandidates {
		if _, err := os.Stat(cand); err == nil {
			base := filepath.Dir(cand)
			pattern := filepath.ToSlash(filepath.Join(base, "*"))
			t, err := template.ParseGlob(pattern)
			if err != nil {
				// Parsing failed for this candidate, try next
				logMessage("DEBUG", fmt.Sprintf("Failed to parse on-disk templates at %s: %v", base, err))
				continue
			}
			// Use disk templates as primary and fill missing names from embedded
			tmpl = t
			if embeddedLoaded {
				_ = registerEmbeddedAsBase(tmpl)
			}
			tmplFound = true
			logMessage("INFO", fmt.Sprintf("Using on-disk templates at %s (overriding embedded)", base))
			break
		}
	}

	if !tmplFound {
		if embeddedLoaded {
			tmpl = embeddedT
			logMessage("INFO", "Using embedded templates from binary")
		} else {
			// Fallback: create a minimal template to avoid crashing the server
			tmpl = template.Must(template.New("index.html").Parse("<html><body><h1>CAKE Auto RTT</h1><p>UI not available</p></body></html>"))
			logMessage("WARN", "No embedded or on-disk templates available; using minimal fallback template")
		}
	}

	// Ensure embedded templates fill any missing names
	_ = registerEmbeddedAsBase(tmpl)

	r.SetHTMLTemplate(tmpl)

	// Main monitoring page
	r.GET("/cake-autortt", ws.handleIndex)
	r.GET("/", ws.handleIndex)

	// API endpoints
	api := r.Group("/api")
	{
		api.GET("/status", ws.handleStatus)
		api.GET("/probes", ws.handleProbes)
		api.GET("/qdisc", ws.handleQdiscStats)
		api.GET("/logs", ws.handleLogs)
	}

	// WebSocket endpoint for real-time updates
	r.GET("/ws", ws.handleWebSocket)

	// Start background goroutine for broadcasting updates
	go ws.broadcastUpdates()

	addr := fmt.Sprintf(":%d", ws.config.WebPort)
	log.Printf("[INFO] Starting web server on %s", addr)
	return r.Run(addr)
}

// handleIndex serves the main monitoring page
func (ws *WebServer) handleIndex(c *gin.Context) {
	// Render using the base template name so it works with both disk and embedded templates.
	c.HTML(http.StatusOK, "index.html", gin.H{
		"title": "CAKE Auto RTT Monitor",
	})
}

// handleStatus returns the current system status
func (ws *WebServer) handleStatus(c *gin.Context) {
	status := ws.getSystemStatus()
	c.JSON(http.StatusOK, status)
}

// handleQdiscStats returns current qdisc statistics
func (ws *WebServer) handleQdiscStats(c *gin.Context) {
	stats := ws.getQdiscStats()
	c.JSON(http.StatusOK, stats)
}

// handleLogs returns recent log messages
func (ws *WebServer) handleLogs(c *gin.Context) {
	logs := ws.getRecentLogs()
	c.JSON(http.StatusOK, logs)
}

// handleProbes returns the current probe statuses
func (ws *WebServer) handleProbes(c *gin.Context) {
	if ws.service == nil {
		c.JSON(http.StatusOK, []ProbeStatus{})
		return
	}
	probes := ws.service.GetCurrentProbes()
	c.JSON(http.StatusOK, probes)
}

// handleWebSocket handles WebSocket connections for real-time updates
func (ws *WebServer) handleWebSocket(c *gin.Context) {
	conn, err := ws.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ERROR] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ws.clientMu.Lock()
	ws.clients[conn] = true
	ws.clientMu.Unlock()

	defer func() {
		ws.clientMu.Lock()
		delete(ws.clients, conn)
		ws.clientMu.Unlock()
	}()

	// Send initial rich status (includes config and probes)
	rich := ws.getRichStatus()
	if err := conn.WriteJSON(rich); err != nil {
		log.Printf("[ERROR] Failed to send initial status: %v", err)
		return
	}

	// Keep connection alive and handle client messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ERROR] WebSocket error: %v", err)
			}
			break
		}
	}
}

// broadcastUpdates sends periodic updates to all connected WebSocket clients
func (ws *WebServer) broadcastUpdates() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rich := ws.getRichStatus()
			ws.broadcastToClients(rich)
		case logMsg := <-ws.logChan:
			// Broadcast new log message immediately
			ws.broadcastToClients(map[string]interface{}{
				"type": "log",
				"data": logMsg,
			})
		}
	}
}

// broadcastToClients sends data to all connected WebSocket clients
func (ws *WebServer) broadcastToClients(data interface{}) {
	ws.clientMu.RLock()
	defer ws.clientMu.RUnlock()

	for client := range ws.clients {
		if err := client.WriteJSON(data); err != nil {
			log.Printf("[ERROR] Failed to send data to client: %v", err)
			client.Close()
			delete(ws.clients, client)
		}
	}
}

// getSystemStatus returns the current system status
func (ws *WebServer) getSystemStatus() WebSystemStatus {
	status := WebSystemStatus{
		Timestamp:     time.Now().Local().Format(time.RFC1123),
		ServiceStatus: "Running",
		ActiveHosts:   0,
		CurrentRTT:    "N/A",
		QdiscStats:    ws.getQdiscStats(),
		RecentLogs:    ws.getRecentLogs(),
	}

	if ws.service != nil {
		// Get system status from service
		sysStatus := ws.service.GetSystemStatus()
		status.ActiveHosts = sysStatus.ActiveHosts
		if len(sysStatus.CurrentRTT) > 0 {
			// Get the most recent RTT value
			for key, rtt := range sysStatus.CurrentRTT {
				status.CurrentRTT = fmt.Sprintf("%s: %dms", key, rtt)
				break // Just show the first one for now
			}
		}
		// keep /api/status simple; richer payloads (config + probes) are sent via WebSocket
	}

	return status
}

// getRichStatus returns a richer status payload suitable for WebSocket clients
func (ws *WebServer) getRichStatus() map[string]interface{} {
	status := ws.getSystemStatus()
	result := map[string]interface{}{
		"type":           "status",
		"timestamp":      status.Timestamp,
		"service_status": status.ServiceStatus,
		"active_hosts":   status.ActiveHosts,
		"current_rtt":    status.CurrentRTT,
		"qdisc_stats":    status.QdiscStats,
		"recent_logs":    status.RecentLogs,
		"config": map[string]interface{}{
			"rtt_update_interval": ws.config.RTTUpdateInterval,
			"min_hosts":           ws.config.MinHosts,
			"max_hosts":           ws.config.MaxHosts,
			"rtt_margin_percent":  ws.config.RTTMarginPercent,
		},
	}

	if ws.service != nil {
		result["probes"] = ws.service.GetCurrentProbes()
	} else {
		result["probes"] = []ProbeStatus{}
	}
	if ws.service != nil {
		// provide completed probes with timestamps for UI
		result["completed_probes"] = ws.service.GetRecentCompletedProbesWithTime()
	} else {
		result["completed_probes"] = []map[string]interface{}{}
	}

	return result
}

// getQdiscStats returns current qdisc statistics
func (ws *WebServer) getQdiscStats() []QdiscStats {
	var stats []QdiscStats

	// Execute tc -s qdisc command
	cmd := exec.Command("tc", "-s", "qdisc")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[ERROR] Failed to get qdisc stats: %v", err)
		return stats
	}

	// Parse output and extract CAKE qdiscs
	lines := strings.Split(string(output), "\n")
	var currentInterface, currentQdisc, currentStats string
	var rttInfo string
	inQdiscBlock := false

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// Empty line might indicate end of qdisc block
			if inQdiscBlock && currentInterface != "" {
				// Check if this is the end of the current qdisc block
				nextLineEmpty := i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == ""
				if nextLineEmpty || (i+1 < len(lines) && strings.Contains(lines[i+1], "qdisc")) {
					// End of current qdisc block, add it to stats
					stats = append(stats, QdiscStats{
						Interface: currentInterface,
						Qdisc:     currentQdisc,
						Stats:     strings.TrimSpace(currentStats),
						RTT:       rttInfo,
					})
					// Reset for next qdisc
					currentInterface = ""
					currentQdisc = ""
					currentStats = ""
					rttInfo = "N/A"
					inQdiscBlock = false
				}
			}
			continue
		}

		if strings.Contains(line, "qdisc cake") {
			// Save previous qdisc if exists
			if currentInterface != "" {
				stats = append(stats, QdiscStats{
					Interface: currentInterface,
					Qdisc:     currentQdisc,
					Stats:     strings.TrimSpace(currentStats),
					RTT:       rttInfo,
				})
			}

			// Start new qdisc
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				currentInterface = parts[4]
				currentQdisc = line
				currentStats = ""
				rttInfo = "N/A"
				inQdiscBlock = true
			}
		} else if inQdiscBlock && currentInterface != "" {
			// Collect all lines that belong to this qdisc
			if strings.HasPrefix(line, "Sent") || strings.HasPrefix(line, "backlog") ||
				strings.Contains(line, "bytes") || strings.Contains(line, "pkt") ||
				strings.Contains(line, "dropped") || strings.Contains(line, "overlimits") {
				currentStats += line + "\n"
			} else if strings.Contains(line, "interval") {
				// Extract RTT/interval information from CAKE output (support us, ms, s)
				rttInfo = ws.extractRTTFromLine(line)
				currentStats += line + "\n"
			} else if strings.Contains(line, "thresh") || strings.Contains(line, "target") ||
				strings.Contains(line, "pkts") || strings.Contains(line, "flows") {
				// Include other CAKE-specific statistics
				currentStats += line + "\n"
			}
		}
	}

	// Add any remaining qdisc
	if currentInterface != "" {
		stats = append(stats, QdiscStats{
			Interface: currentInterface,
			Qdisc:     currentQdisc,
			Stats:     strings.TrimSpace(currentStats),
			RTT:       rttInfo,
		})
	}

	return stats
}

// extractRTTFromLine extracts RTT information from a tc output line
func (ws *WebServer) extractRTTFromLine(line string) string {
	// Find candidate token after 'interval' or 'rtt'
	parts := strings.Fields(line)
	var candidate string
	for i, part := range parts {
		if part == "interval" && i+1 < len(parts) {
			candidate = strings.Trim(parts[i+1], ",;")
			break
		}
	}
	if candidate == "" {
		for i, part := range parts {
			if part == "rtt" && i+1 < len(parts) {
				candidate = strings.Trim(parts[i+1], ",;")
				break
			}
		}
	}
	if candidate == "" {
		return "N/A"
	}

	// Parse numeric value and unit (supports floats) - e.g. 1.42s, 100ms, 500us
	re := regexp.MustCompile(`^([0-9]*\.?[0-9]+)(us|ms|s)?$`)
	matches := re.FindStringSubmatch(candidate)
	if len(matches) < 2 {
		// couldn't parse, return raw candidate
		return candidate
	}

	valStr := matches[1]
	unit := matches[2]
	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return candidate
	}

	switch unit {
	case "us":
		// microseconds -> milliseconds
		ms := v / 1000.0
		if ms < 1.0 {
			// keep in microseconds for precision
			return fmt.Sprintf("%dus", int(math.Round(v)))
		}
		return fmt.Sprintf("%dms", int(math.Round(ms)))
	case "", "ms":
		return fmt.Sprintf("%dms", int(math.Round(v)))
	case "s":
		ms := v * 1000.0
		return fmt.Sprintf("%dms", int(math.Round(ms)))
	default:
		return candidate
	}
}

// getRecentLogs returns recent log messages
func (ws *WebServer) getRecentLogs() []LogMessage {
	// Prefer service-provided logs when available
	if ws.service != nil {
		entries := ws.service.GetRecentLogs()
		out := make([]LogMessage, 0, len(entries))
		for _, e := range entries {
			out = append(out, LogMessage{
				Timestamp: e.Timestamp.Format("15:04:05"),
				Level:     e.Level,
				Message:   e.Message,
			})
		}
		return out
	}

	// Fallback
	return []LogMessage{
		{
			Timestamp: time.Now().Local().Format("15:04:05"),
			Level:     "INFO",
			Message:   "Service is running normally",
		},
	}
}

// LogInfo sends an info log message to the web interface
func (ws *WebServer) LogInfo(message string) {
	logMsg := LogMessage{
		Timestamp: time.Now().Local().Format("15:04:05"),
		Level:     "INFO",
		Message:   message,
	}
	select {
	case ws.logChan <- logMsg:
	default:
		// Channel is full, skip this log
	}
}

// LogError sends an error log message to the web interface
func (ws *WebServer) LogError(message string) {
	logMsg := LogMessage{
		Timestamp: time.Now().Local().Format("15:04:05"),
		Level:     "ERROR",
		Message:   message,
	}
	select {
	case ws.logChan <- logMsg:
	default:
		// Channel is full, skip this log
	}
}
