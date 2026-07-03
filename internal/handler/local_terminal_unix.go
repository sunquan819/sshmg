//go:build !windows

package handler

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type realPTY struct {
	file *os.File
}

type pty struct {
	f *realPTY
}

func openPTY(cmd *exec.Cmd, cols, rows uint16) (*pty, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, err
	}
	return &pty{f: &realPTY{file: f}}, nil
}

func (p *pty) Read(buf []byte) (int, error)  { return p.f.file.Read(buf) }
func (p *pty) Write(buf []byte) (int, error) { return p.f.file.Write(buf) }
func (p *pty) close()                        { p.f.file.Close() }
func (p *pty) resize(cols, rows uint16) {
	pty.Setsize(p.f.file, &pty.Winsize{Cols: cols, Rows: rows})
}
