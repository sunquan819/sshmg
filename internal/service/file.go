package service

import (
	"encoding/base64"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"deploy-manager/internal/model"
	sshPkg "deploy-manager/pkg/ssh"
	"golang.org/x/crypto/ssh"
)

type FileService struct{}

var FileSvc = &FileService{}

type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
	Mode    string    `json:"mode"`
}

func (s *FileService) ListFiles(server *model.Server, path string) ([]FileInfo, error) {
	path = strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	cmd := fmt.Sprintf(`ls -la "%s" 2>&1`, path)
	log.Printf("[FileService] ListFiles: server=%s, user=%s, path=%s", server.IP, server.Username, path)
	output, err := SSHSvc.ExecuteCommand(server, cmd, 15*time.Second)
	log.Printf("[FileService] ListFiles output: %s, err: %v", output, err)

	// Check output for errors even if err is nil
	fullOutput := output
	if err != nil {
		fullOutput = output + err.Error()
	}
	log.Printf("[FileService] ListFiles fullOutput: %s", fullOutput)

	if strings.Contains(fullOutput, "权限不够") || strings.Contains(fullOutput, "权限不足") || strings.Contains(fullOutput, "permission denied") {
		return nil, fmt.Errorf("权限不足，无法访问: %s", path)
	}
	if strings.Contains(fullOutput, "No such file or directory") || strings.Contains(fullOutput, "不存在") {
		return nil, fmt.Errorf("目录不存在: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("无法获取文件列表: %s", fullOutput)
	}

	var files []FileInfo
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 || strings.HasPrefix(line, "total") {
			continue
		}
		info := parseLsLine(line, path)
		if info != nil {
			files = append(files, *info)
		}
	}

	return files, nil
}

func parseLsLine(line, basePath string) *FileInfo {
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return nil
	}

	mode := fields[0]
	if len(mode) < 10 {
		return nil
	}

	// The name is the last field (works even with spaces in names)
	name := fields[len(fields)-1]

	// Skip . and ..
	if name == "." || name == ".." {
		return nil
	}

	// Check if it's a directory or symlink
	isDir := mode[0] == 'd' || mode[0] == 'l'

	// For symlinks, find the -> and extract target
	if mode[0] == 'l' {
		arrowIdx := -1
		for i, f := range fields {
			if f == "->" {
				arrowIdx = i
				break
			}
		}
		if arrowIdx > 0 {
			// Name is from field after date/time (usually field 8) up to ->
			// For ls -la format: mode, links, user, group, size, date, time, name, ->, target
			name = fields[arrowIdx-1]
			if arrowIdx+1 < len(fields) {
				linkTarget := strings.Join(fields[arrowIdx+1:], " ")
				name = name + " -> " + linkTarget
			}
		}
	}

	// Get size - for directories it shows block count
	size := int64(0)
	if !isDir {
		size = parseInt64(fields[4])
	}

	path := basePath
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	path += name

	return &FileInfo{
		Name:  name,
		Path:  path,
		Size:  size,
		IsDir: isDir,
		Mode:  mode,
	}
}

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

func (s *FileService) ReadFile(server *model.Server, path string, limit int64) (string, error) {
	var cmd string
	if limit > 0 {
		cmd = fmt.Sprintf(`head -c %d "%s"`, limit, path)
	} else {
		cmd = fmt.Sprintf(`cat "%s"`, path)
	}

	return SSHSvc.ExecuteCommand(server, cmd, 30*time.Second)
}

func (s *FileService) WriteFile(server *model.Server, path, content string) error {
	cmd := fmt.Sprintf(`cat > "%s" << 'EOFMARKER'
%s
EOFMARKER`, path, content)

	_, err := SSHSvc.ExecuteCommand(server, cmd, 30*time.Second)
	return err
}

func (s *FileService) DeleteFile(server *model.Server, path string) error {
	cmd := fmt.Sprintf(`rm -rf "%s"`, path)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *FileService) CreateDirectory(server *model.Server, path string) error {
	cmd := fmt.Sprintf(`mkdir -p "%s"`, path)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *FileService) MoveFile(server *model.Server, src, dst string) error {
	cmd := fmt.Sprintf(`mv "%s" "%s"`, src, dst)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *FileService) CopyFile(server *model.Server, src, dst string) error {
	cmd := fmt.Sprintf(`cp -r "%s" "%s"`, src, dst)
	_, err := SSHSvc.ExecuteCommand(server, cmd, 10*time.Second)
	return err
}

func (s *FileService) UploadFile(server *model.Server, remotePath string, data []byte) (string, error) {
	client, err := s.getNewClient(server)
	if err != nil {
		return "", err
	}

	actualPath, err := s.getUniqueFilePath(server, remotePath)
	if err != nil {
		return "", err
	}

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	err = session.Start(fmt.Sprintf("cat > %q", actualPath))
	if err != nil {
		return "", fmt.Errorf("failed to start remote command: %w", err)
	}

	_, err = stdin.Write(data)
	if err != nil {
		stdin.Close()
		session.Close()
		return "", fmt.Errorf("failed to write data: %w", err)
	}
	stdin.Close()

	err = session.Wait()
	if err != nil {
		return "", fmt.Errorf("remote command failed: %w", err)
	}

	return actualPath, nil
}

func (s *FileService) getUniqueFilePath(server *model.Server, path string) (string, error) {
	path = strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	checkCmd := fmt.Sprintf(`test -e "%s" && echo "exists" || echo "notexists"`, path)
	output, err := SSHSvc.ExecuteCommand(server, checkCmd, 5*time.Second)
	if err != nil {
		return path, nil
	}

	if strings.TrimSpace(output) != "exists" {
		return path, nil
	}

	dir := path
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash > 0 {
		dir = path[:lastSlash]
	}
	baseName := path[lastSlash+1:]

	ext := ""
	if idx := strings.LastIndex(baseName, "."); idx > 0 {
		ext = baseName[idx:]
		baseName = baseName[:idx]
	}

	for i := 1; i <= 100; i++ {
		newName := fmt.Sprintf("%s(%d)%s", baseName, i, ext)
		newPath := dir + "/" + newName
		checkCmd := fmt.Sprintf(`test -e "%s" && echo "exists" || echo "notexists"`, newPath)
		output, err := SSHSvc.ExecuteCommand(server, checkCmd, 5*time.Second)
		if err != nil {
			continue
		}
		if strings.TrimSpace(output) != "exists" {
			return newPath, nil
		}
	}

	return path, fmt.Errorf("cannot find unique filename")
}

func (s *FileService) getNewClient(server *model.Server) (*ssh.Client, error) {
	sshClient := sshPkg.NewClient(server.IP, server.Port, server.Username, server.Password, server.SSHKey)
	sshClient.JumpEnabled = server.JumpEnabled
	sshClient.JumpHost = server.JumpIP
	sshClient.JumpPort = server.JumpPort
	sshClient.JumpUser = server.JumpUser
	sshClient.JumpPassword = server.JumpPassword
	sshClient.JumpKey = server.JumpKey
	sshClient.ProxyEnabled = server.ProxyEnabled
	sshClient.ProxyType = server.ProxyType
	sshClient.ProxyHost = server.ProxyHost
	sshClient.ProxyPort = server.ProxyPort

	if server.JumpServerID > 0 {
		chain, err := SSHSvc.BuildJumpChain(server)
		if err != nil {
			return nil, fmt.Errorf("failed to build jump chain: %w", err)
		}
		sshClient.JumpChain = chain
		sshClient.JumpEnabled = true
	}

	if err := sshClient.Connect(); err != nil {
		return nil, err
	}
	return sshClient.GetNativeClient()
}

func (s *FileService) DownloadFile(server *model.Server, remotePath string) ([]byte, error) {
	remotePath = strings.ReplaceAll(remotePath, "\\", "/")
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}

	cmd := fmt.Sprintf(`base64 "%s"`, remotePath)
	output, err := SSHSvc.ExecuteCommand(server, cmd, 30*time.Second)
	if err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		return nil, fmt.Errorf("decode base64 error: %v", err)
	}
	return data, nil
}

func (s *FileService) GetFileStat(server *model.Server, path string) (*FileInfo, error) {
	cmd := fmt.Sprintf(`stat -c "%%s %%Y %%a" "%s"`, path)
	output, err := SSHSvc.ExecuteCommand(server, cmd, 5*time.Second)
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid stat output")
	}

	return &FileInfo{
		Path: path,
		Name: filepath.Base(path),
		Size: parseInt64(fields[0]),
		Mode: fields[2],
	}, nil
}

func (s *FileService) SearchFiles(server *model.Server, path, pattern string) ([]string, error) {
	cmd := fmt.Sprintf(`find "%s" -name "%s" 2>/dev/null`, path, pattern)
	output, err := SSHSvc.ExecuteCommand(server, cmd, 30*time.Second)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var results []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			results = append(results, line)
		}
	}

	return results, nil
}
