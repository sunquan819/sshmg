package ssh

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pkg/sftp"
)

func (c *Client) getSFTPClient() (*sftp.Client, error) {
	sshClient, err := c.GetNativeClient()
	if err != nil {
		return nil, err
	}
	return sftp.NewClient(sshClient)
}

func (c *Client) UploadFile(localPath, remotePath string) error {
	sftpClient, err := c.getSFTPClient()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	localFile, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	remoteDir := remotePath[:strings.LastIndex(remotePath, "/")]
	if remoteDir != "" {
		if err := sftpClient.MkdirAll(remoteDir); err != nil {
			fmt.Printf("Warning: failed to create remote directory: %v\n", err)
		}
	}

	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, localFile)
	return err
}

func (c *Client) DownloadFile(remotePath, localPath string) error {
	sftpClient, err := c.getSFTPClient()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	localFile, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, remoteFile)
	return err
}

// DownloadToWriter 用 SFTP 流式读远程文件到任意 io.Writer(避免大文件爆内存)
// 不需要落本地,直接给 HTTP response 写就行
func (c *Client) DownloadToWriter(remotePath string, w io.Writer) error {
	sftpClient, err := c.getSFTPClient()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		return err
	}
	defer remoteFile.Close()

	_, err = io.Copy(w, remoteFile)
	return err
}

func (c *Client) RemoteFileExists(remotePath string) (bool, error) {
	sftpClient, err := c.getSFTPClient()
	if err != nil {
		return false, err
	}
	defer sftpClient.Close()

	_, err = sftpClient.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Client) RemoteMkdir(remotePath string) error {
	sftpClient, err := c.getSFTPClient()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	return sftpClient.MkdirAll(remotePath)
}

func (c *Client) UploadDir(localDir, remoteDir string) error {
	sftpClient, err := c.getSFTPClient()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	if err := sftpClient.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("failed to create remote directory: %w", err)
	}

	return uploadDirRecursive(sftpClient, localDir, remoteDir)
}

func uploadDirRecursive(sftpClient *sftp.Client, localDir, remoteDir string) error {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		localPath := localDir + "/" + entry.Name()
		remotePath := remoteDir + "/" + entry.Name()

		if entry.IsDir() {
			if err := sftpClient.MkdirAll(remotePath); err != nil {
				return err
			}
			if err := uploadDirRecursive(sftpClient, localPath, remotePath); err != nil {
				return err
			}
		} else {
			localFile, err := os.Open(localPath)
			if err != nil {
				return err
			}
			defer localFile.Close()

			f, err := sftpClient.Create(remotePath)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(f, localFile); err != nil {
				return err
			}
		}
	}
	return nil
}
