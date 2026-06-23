package ssh

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type JumpServer struct {
	Host     string
	Port     int
	User     string
	Password string
	Key      string
}

type Client struct {
	Host         string
	Port         int
	Username     string
	Password     string
	PrivateKey   string
	JumpEnabled  bool
	JumpHost     string
	JumpPort     int
	JumpUser     string
	JumpPassword string
	JumpKey      string
	JumpChain    []JumpServer
	ProxyEnabled bool
	ProxyType    string
	ProxyHost    string
	ProxyPort    int
	client       *ssh.Client
	jumpClient   *ssh.Client
	mu           sync.RWMutex
}

type SessionConfig struct {
	Cmd     string
	Env     map[string]string
	Timeout time.Duration
}

type SessionResult struct {
	Output   string
	Error    string
	ExitCode int
}

func NewClient(host string, port int, username, password, privateKey string) *Client {
	return &Client{
		Host:       host,
		Port:       port,
		Username:   username,
		Password:   password,
		PrivateKey: privateKey,
	}
}

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Close()
	}
	if c.jumpClient != nil {
		c.jumpClient.Close()
	}

	if c.JumpEnabled && c.JumpHost != "" {
		return c.connectViaJump()
	}

	return c.connectDirect()
}

func (c *Client) connectDirect() error {
	config := &ssh.ClientConfig{
		User: c.Username,
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		Timeout: 10 * time.Second,
	}

	if c.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(c.PrivateKey))
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	} else if c.Password != "" {
		config.Auth = append(config.Auth, ssh.Password(c.Password))
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)

	var conn net.Conn
	var err error

	if c.ProxyEnabled && c.ProxyHost != "" {
		log.Printf("[SSH] 连接代理: %s:%d -> %s (类型: %s)", c.ProxyHost, c.ProxyPort, addr, c.ProxyType)
		conn, err = c.dialViaProxy(addr)
	} else {
		log.Printf("[SSH] 直连: %s", addr)
		// 加 10s TCP 拨号 timeout,避免对端不可达时永久阻塞
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}

	if err != nil {
		log.Printf("[SSH] TCP拨号失败 %s: %v", addr, err)
		return fmt.Errorf("failed to connect: %w", err)
	}
	log.Printf("[SSH] TCP连接建立 %s,开始 SSH 握手...", addr)

	// 启用 TCP keepalive,防止中间设备(NAT/防火墙)静默断开连接
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	// SSH 握手整体加 timeout(底层 net.Conn 也要 deadline,否则协议层可能永久阻塞)
	allConn := conn
	if tcpConn, ok := allConn.(*net.TCPConn); ok {
		tcpConn.SetDeadline(time.Now().Add(15 * time.Second))
		defer tcpConn.SetDeadline(time.Time{}) // 清除 deadline,后续通信不受影响
	}

	client, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		log.Printf("[SSH] SSH握手失败 %s: %v", addr, err)
		return fmt.Errorf("failed to start ssh handshake: %w", err)
	}
	log.Printf("[SSH] SSH握手成功 %s", addr)

	c.client = ssh.NewClient(client, chans, reqs)
	return nil
}

func (c *Client) dialViaProxy(addr string) (net.Conn, error) {
	proxyAddr := fmt.Sprintf("%s:%d", c.ProxyHost, c.ProxyPort)

	switch c.ProxyType {
	case "socks5":
		return c.dialSocks5Proxy(proxyAddr, addr)
	case "http":
		return c.dialHttpProxy(proxyAddr, addr)
	default:
		return c.dialSocks5Proxy(proxyAddr, addr)
	}
}

func (c *Client) dialSocks5Proxy(proxyAddr, targetAddr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	buf := []byte{5, 1, 0}
	if _, err := conn.Write(buf); err != nil {
		conn.Close()
		return nil, err
	}

	resp := make([]byte, 2)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, err
	}

	if resp[0] != 5 || resp[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 auth failed")
	}

	req := []byte{5, 1, 0, 3}
	host, port := parseAddr(targetAddr)
	req = append(req, host...)
	req = append(req, port...)

	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}

	resp = make([]byte, 10)
	if _, err := conn.Read(resp); err != nil {
		conn.Close()
		return nil, err
	}

	if resp[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect failed")
	}

	return conn, nil
}

func (c *Client) dialHttpProxy(proxyAddr, targetAddr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, err
	}

	resp := string(buf[:n])
	if !strings.Contains(resp, "200") {
		conn.Close()
		return nil, fmt.Errorf("http proxy connect failed: %s", resp)
	}

	return conn, nil
}

func parseAddr(addr string) ([]byte, []byte) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []byte{0}, []byte{0, 0}
	}

	portNum := 0
	fmt.Sscanf(port, "%d", &portNum)

	hostBytes := []byte{byte(len(host))}
	hostBytes = append(hostBytes, []byte(host)...)
	portBytes := []byte{byte(portNum >> 8), byte(portNum & 0xff)}

	return hostBytes, portBytes
}

func (c *Client) connectViaJump() error {
	var jumpServers []JumpServer

	log.Printf("[SSH] JumpChain长度: %d, JumpHost: %s, JumpEnabled: %v", len(c.JumpChain), c.JumpHost, c.JumpEnabled)

	if len(c.JumpChain) > 0 {
		jumpServers = c.JumpChain
		for i, j := range jumpServers {
			log.Printf("[SSH] 跳板机链 %d: %s:%d user:%s", i+1, j.Host, j.Port, j.User)
		}
	} else if c.JumpHost != "" {
		jumpServers = []JumpServer{
			{Host: c.JumpHost, Port: c.JumpPort, User: c.JumpUser, Password: c.JumpPassword, Key: c.JumpKey},
		}
		log.Printf("[SSH] 使用旧方式跳板机: %s:%d", c.JumpHost, c.JumpPort)
	}

	if len(jumpServers) == 0 {
		return fmt.Errorf("no jump servers configured")
	}

	log.Printf("[SSH] 连接 %d 级跳板机", len(jumpServers))

	var currentClient *ssh.Client
	var err error

	for i, jump := range jumpServers {
		log.Printf("[SSH] 跳板机 %d: %s:%d", i+1, jump.Host, jump.Port)

		jumpConfig := &ssh.ClientConfig{
			User: jump.User,
			Auth: []ssh.AuthMethod{},
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
				return nil
			},
			Timeout: 10 * time.Second,
		}

		if jump.Key != "" {
			signer, err := ssh.ParsePrivateKey([]byte(jump.Key))
			if err != nil {
				return fmt.Errorf("failed to parse jump private key: %w", err)
			}
			jumpConfig.Auth = append(jumpConfig.Auth, ssh.PublicKeys(signer))
		} else if jump.Password != "" {
			jumpConfig.Auth = append(jumpConfig.Auth, ssh.Password(jump.Password))
		}

		jumpAddr := fmt.Sprintf("%s:%d", jump.Host, jump.Port)

		if currentClient == nil {
			var tcpConn net.Conn
			tcpConn, err = net.DialTimeout("tcp", jumpAddr, 10*time.Second)
			if err != nil {
				return fmt.Errorf("failed to connect to jump server %s: %w", jump.Host, err)
			}
			if tc, ok := tcpConn.(*net.TCPConn); ok {
				tc.SetKeepAlive(true)
				tc.SetKeepAlivePeriod(30 * time.Second)
			}
			sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, jumpAddr, jumpConfig)
			if err != nil {
				tcpConn.Close()
				return fmt.Errorf("failed to establish SSH connection to jump server %s: %w", jump.Host, err)
			}
			currentClient = ssh.NewClient(sshConn, chans, reqs)
		} else {
			var conn net.Conn
			conn, err = currentClient.Dial("tcp", jumpAddr)
			if err != nil {
				return fmt.Errorf("failed to dial jump server %s via previous jump: %w", jump.Host, err)
			}

			var sshConn ssh.Conn
			sshConn, _, _, err = ssh.NewClientConn(conn, jumpAddr, jumpConfig)
			if err != nil {
				return fmt.Errorf("failed to establish SSH connection to jump server %s: %w", jump.Host, err)
			}
			currentClient = ssh.NewClient(sshConn, nil, nil)
		}
	}

	if c.jumpClient != nil {
		c.jumpClient.Close()
	}
	c.jumpClient = currentClient

	log.Printf("[SSH] 跳板机连接完成，准备连接目标服务器: %s:%d", c.Host, c.Port)

	targetAddr := fmt.Sprintf("%s:%d", c.Host, c.Port)

	targetConfig := &ssh.ClientConfig{
		User: c.Username,
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		Timeout: 10 * time.Second,
	}

	if c.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(c.PrivateKey))
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}
		targetConfig.Auth = append(targetConfig.Auth, ssh.PublicKeys(signer))
	} else if c.Password != "" {
		targetConfig.Auth = append(targetConfig.Auth, ssh.Password(c.Password))
	}

	conn, err := currentClient.Dial("tcp", targetAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to target via jump: %w", err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, targetAddr, targetConfig)
	if err != nil {
		return fmt.Errorf("failed to establish SSH connection via jump: %w", err)
	}

	c.client = ssh.NewClient(sshConn, chans, reqs)
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.jumpClient != nil {
		c.jumpClient.Close()
		c.jumpClient = nil
	}

	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}

func (c *Client) GetNativeClient() (*ssh.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	return c.client, nil
}

func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client != nil
}

func (c *Client) Execute(cmd string, timeout time.Duration) (*SessionResult, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	if timeout > 0 {
		session.Setenv("LC_TIMEOUT", fmt.Sprintf("%d", int(timeout.Seconds())))
	}

	output, err := session.CombinedOutput(cmd)
	result := &SessionResult{
		Output: string(output),
	}

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
			result.Error = exitErr.Error()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	}

	return result, nil
}

func (c *Client) StartShell() (*ssh.Session, io.ReadWriteCloser, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, nil, fmt.Errorf("not connected")
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", 80, 40, modes); err != nil {
		session.Close()
		return nil, nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		return nil, nil, err
	}

	return session, &rwCloser{stdin: stdin, stdout: stdout, session: session}, nil
}

type rwCloser struct {
	stdin   io.WriteCloser
	stdout  io.Reader
	session *ssh.Session
}

func (r *rwCloser) Read(p []byte) (n int, err error) {
	return r.stdout.Read(p)
}

func (r *rwCloser) Write(p []byte) (n int, err error) {
	return r.stdin.Write(p)
}

func (r *rwCloser) Close() error {
	r.stdin.Close()
	return r.session.Close()
}

func (c *Client) TestConnection() error {
	result, err := c.Execute("echo 'connection test'", 5*time.Second)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("connection test failed: %s", result.Error)
	}
	return nil
}

func (c *Client) DynamicPortForward(localPort int) (int, error) {
	if c.client == nil {
		if err := c.Connect(); err != nil {
			return 0, err
		}
	}

	listener, err := c.client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to create listener: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		defer listener.Close()
		for {
			localConn, err := listener.Accept()
			if err != nil {
				break
			}
			go func() {
				defer localConn.Close()
				remoteConn, err := c.client.Dial("tcp", "127.0.0.1:80")
				if err != nil {
					return
				}
				defer remoteConn.Close()
				go io.Copy(remoteConn, localConn)
				go io.Copy(localConn, remoteConn)
			}()
		}
	}()

	log.Printf("Dynamic port forward started on local port %d -> remote", port)
	return port, nil
}

func (c *Client) LocalPortForward(remoteHost string, remotePort int) (int, error) {
	if c.client == nil {
		if err := c.Connect(); err != nil {
			return 0, err
		}
	}

	listener, err := c.client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to listen on local port: %w", err)
	}

	go func() {
		for {
			localConn, err := listener.Accept()
			if err != nil {
				break
			}
			go func() {
				targetAddr := fmt.Sprintf("%s:%d", remoteHost, remotePort)
				remoteConn, err := c.client.Dial("tcp", targetAddr)
				if err != nil {
					localConn.Close()
					return
				}
				go func() {
					io.Copy(localConn, remoteConn)
					localConn.Close()
					remoteConn.Close()
				}()
				go func() {
					io.Copy(remoteConn, localConn)
					remoteConn.Close()
					localConn.Close()
				}()
			}()
		}
	}()

	return listener.Addr().(*net.TCPAddr).Port, nil
}
