package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"deploy-manager/internal/database"
	"deploy-manager/internal/model"
	"deploy-manager/internal/service"

	"github.com/gin-gonic/gin"
)

type PacketCaptureHandler struct{}

func NewPacketCaptureHandler() *PacketCaptureHandler {
	return &PacketCaptureHandler{}
}

type PacketCaptureRequest struct {
	TargetType string `json:"target_type"`
	ServerID   uint   `json:"server_id"`
	Interface  string `json:"interface"`
	Port       string `json:"port"`
	Host       string `json:"host"`
	Protocol   string `json:"protocol"`
	Count      int    `json:"count"`
	Duration   int    `json:"duration"`
}

type PacketCaptureResponse struct {
	Command string               `json:"command"`
	Output  string               `json:"output"`
	Error   string               `json:"error,omitempty"`
	Records []PacketRecord       `json:"records"`
	Summary PacketCaptureSummary `json:"summary"`
}

type PacketRecord struct {
	Time        string `json:"time"`
	Protocol    string `json:"protocol"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Length      string `json:"length"`
	Summary     string `json:"summary"`
	Raw         string `json:"raw"`
}

type PacketCaptureSummary struct {
	Total      int            `json:"total"`
	Protocols  map[string]int `json:"protocols"`
	TopTalkers []TalkerCount  `json:"top_talkers"`
}

type TalkerCount struct {
	Address string `json:"address"`
	Count   int    `json:"count"`
}

func (h *PacketCaptureHandler) Run(c *gin.Context) {
	var req PacketCaptureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalized, err := normalizePacketCaptureRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var output, command string
	if normalized.TargetType == "server" {
		var server model.Server
		if err := database.DB.First(&server, normalized.ServerID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		command = buildRemotePacketCaptureCommand(normalized)
		output, err = service.SSHSvc.ExecuteCommand(&server, command, time.Duration(normalized.Duration+8)*time.Second)
	} else {
		command, output, err = runLocalPacketCapture(c.Request.Context(), normalized)
	}

	records := parsePacketCaptureOutput(output)
	resp := PacketCaptureResponse{
		Command: command,
		Output:  output,
		Records: records,
		Summary: summarizePacketRecords(records),
	}
	if err != nil {
		resp.Error = friendlyPacketCaptureError(err, normalized.TargetType)
	}
	c.JSON(http.StatusOK, resp)
}

func normalizePacketCaptureRequest(req PacketCaptureRequest) (PacketCaptureRequest, error) {
	req.TargetType = strings.ToLower(strings.TrimSpace(req.TargetType))
	if req.TargetType == "" {
		req.TargetType = "local"
	}
	if req.TargetType != "local" && req.TargetType != "server" {
		return req, fmt.Errorf("target_type must be local or server")
	}
	if req.TargetType == "server" && req.ServerID == 0 {
		return req, fmt.Errorf("server_id is required for server capture")
	}

	req.Interface = strings.TrimSpace(req.Interface)
	if req.Interface == "" {
		req.Interface = "any"
	}
	if !isSafeCaptureToken(req.Interface) {
		return req, fmt.Errorf("invalid interface")
	}

	req.Port = strings.TrimSpace(req.Port)
	if req.Port != "" {
		port, err := strconv.Atoi(req.Port)
		if err != nil || port < 1 || port > 65535 {
			return req, fmt.Errorf("port must be between 1 and 65535")
		}
		req.Port = strconv.Itoa(port)
	}

	req.Host = strings.TrimSpace(req.Host)
	if req.Host != "" && !isValidCaptureHost(req.Host) {
		return req, fmt.Errorf("invalid host")
	}

	req.Protocol = strings.ToLower(strings.TrimSpace(req.Protocol))
	if req.Protocol == "" {
		req.Protocol = "any"
	}
	switch req.Protocol {
	case "any", "tcp", "udp", "icmp":
	default:
		return req, fmt.Errorf("protocol must be any, tcp, udp or icmp")
	}

	if req.Count <= 0 {
		req.Count = 80
	}
	if req.Count > 500 {
		req.Count = 500
	}
	if req.Duration <= 0 {
		req.Duration = 20
	}
	if req.Duration > 60 {
		req.Duration = 60
	}
	return req, nil
}

func buildPacketCaptureFilter(req PacketCaptureRequest) string {
	var filters []string
	if req.Protocol != "" && req.Protocol != "any" {
		filters = append(filters, req.Protocol)
	}
	if req.Port != "" {
		filters = append(filters, "port "+req.Port)
	}
	if req.Host != "" {
		filters = append(filters, "host "+req.Host)
	}
	if len(filters) == 0 {
		return "ip or ip6"
	}
	return strings.Join(filters, " and ")
}

func buildRemotePacketCaptureCommand(req PacketCaptureRequest) string {
	filter := buildPacketCaptureFilter(req)
	return fmt.Sprintf("timeout %d tcpdump -i %s -c %d -nn -tttt %s 2>&1", req.Duration+3, req.Interface, req.Count, shellQuoteCaptureFilter(filter))
}

func runLocalPacketCapture(parent context.Context, req PacketCaptureRequest) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(req.Duration+8)*time.Second)
	defer cancel()

	filter := buildPacketCaptureFilter(req)
	if path, ok := firstExistingCommand("tshark"); ok {
		args := []string{"-i", req.Interface, "-c", strconv.Itoa(req.Count), "-a", fmt.Sprintf("duration:%d", req.Duration), "-f", filter, "-n"}
		output, err := runCommand(ctx, path, args...)
		return path + " " + strings.Join(args, " "), output, err
	}
	if path, ok := firstExistingCommand("tcpdump", "windump"); ok {
		args := []string{"-i", req.Interface, "-c", strconv.Itoa(req.Count), "-nn", "-tttt", filter}
		output, err := runCommand(ctx, path, args...)
		return path + " " + strings.Join(args, " "), output, err
	}
	return "", "", fmt.Errorf("no local packet capture tool found")
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("packet capture timed out")
	}
	return string(output), err
}

func firstExistingCommand(names ...string) (string, bool) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	return "", false
}

var tcpdumpLineRE = regexp.MustCompile(`^(?:(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+)?(?:IP6?|ARP)\s+(.+?)\s+>\s+(.+?):\s*(.*)$`)
var lengthRE = regexp.MustCompile(`(?i)(?:length|len)\s+(\d+)`)

func parsePacketCaptureOutput(output string) []PacketRecord {
	lines := strings.Split(output, "\n")
	records := make([]PacketRecord, 0, len(lines))
	for _, line := range lines {
		raw := strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "tcpdump: listening on") || strings.HasPrefix(trimmed, "dropped privs") {
			continue
		}
		record := parsePacketLine(trimmed)
		if record.Raw == "" {
			record.Raw = trimmed
		}
		records = append(records, record)
	}
	return records
}

func parsePacketLine(line string) PacketRecord {
	record := PacketRecord{Raw: line}
	matches := tcpdumpLineRE.FindStringSubmatch(line)
	if len(matches) < 5 {
		record.Summary = line
		record.Protocol = detectProtocol(line)
		return record
	}

	record.Time = matches[1]
	record.Source = normalizeEndpoint(matches[2])
	record.Destination = normalizeEndpoint(matches[3])
	detail := strings.TrimSpace(matches[4])
	record.Protocol = detectProtocol(detail)
	record.Summary = detail
	if length := lengthRE.FindStringSubmatch(detail); len(length) == 2 {
		record.Length = length[1]
	}
	return record
}

func detectProtocol(text string) string {
	upper := strings.ToUpper(text)
	switch {
	case strings.Contains(upper, " UDP") || strings.HasPrefix(upper, "UDP"):
		return "UDP"
	case strings.Contains(upper, " ICMP") || strings.HasPrefix(upper, "ICMP"):
		return "ICMP"
	case strings.Contains(upper, "FLAGS ") || strings.Contains(upper, " SEQ ") || strings.Contains(upper, " ACK "):
		return "TCP"
	case strings.Contains(upper, "ARP"):
		return "ARP"
	default:
		return "OTHER"
	}
}

func normalizeEndpoint(endpoint string) string {
	return strings.Trim(strings.TrimSpace(endpoint), ",")
}

func summarizePacketRecords(records []PacketRecord) PacketCaptureSummary {
	summary := PacketCaptureSummary{
		Total:     len(records),
		Protocols: map[string]int{},
	}
	talkers := map[string]int{}
	for _, record := range records {
		protocol := record.Protocol
		if protocol == "" {
			protocol = "OTHER"
		}
		summary.Protocols[protocol]++
		if record.Source != "" {
			talkers[record.Source]++
		}
		if record.Destination != "" {
			talkers[record.Destination]++
		}
	}
	items := make([]TalkerCount, 0, len(talkers))
	for address, count := range talkers {
		items = append(items, TalkerCount{Address: address, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Address < items[j].Address
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 5 {
		items = items[:5]
	}
	summary.TopTalkers = items
	return summary
}

func friendlyPacketCaptureError(err error, targetType string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "no local packet capture tool") {
		return "本机未找到抓包工具。Windows 建议安装 Wireshark/Npcap 并确保 tshark 在 PATH 中，或安装 Windump；Linux/macOS 可安装 tcpdump。"
	}
	if strings.Contains(strings.ToLower(msg), "permission") || strings.Contains(strings.ToLower(msg), "access is denied") {
		return "抓包权限不足。请用管理员权限启动客户端，或在服务器上给 tcpdump/dumpcap 配置抓包权限。"
	}
	if targetType == "server" && (strings.Contains(msg, "not found") || strings.Contains(msg, "executable file not found")) {
		return "服务器上未找到 tcpdump，请先安装 tcpdump 后再抓包。"
	}
	return msg
}

func isSafeCaptureToken(value string) bool {
	if value == "" {
		return true
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func isValidCaptureHost(value string) bool {
	if net.ParseIP(value) != nil {
		return true
	}
	if len(value) > 253 || strings.ContainsAny(value, " \t\r\n'\";&|`$()<>") {
		return false
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func shellQuoteCaptureFilter(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
