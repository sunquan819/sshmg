//go:build windows

package handler

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/UserExistsError/conpty"
)

type pty struct {
	*conpty.ConPty
}

func openPTY(cmd *exec.Cmd, cols, rows uint16) (*pty, error) {
	args := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		args = append(args, quoteWindowsCommandLineArg(arg))
	}
	cmdLine := strings.Join(args, " ")
	if cmdLine == "" {
		cmdLine = quoteWindowsCommandLineArg(cmd.Path)
	}
	cpty, err := conpty.Start(cmdLine, conpty.ConPtyDimensions(int(cols), int(rows)))
	if err != nil {
		return nil, fmt.Errorf("conpty.Start: %w", err)
	}
	return &pty{cpty}, nil
}

func quoteWindowsCommandLineArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat("\\", backslashes*2+1))
			b.WriteRune(r)
			backslashes = 0
		default:
			if backslashes > 0 {
				b.WriteString(strings.Repeat("\\", backslashes))
				backslashes = 0
			}
			b.WriteRune(r)
		}
	}
	if backslashes > 0 {
		b.WriteString(strings.Repeat("\\", backslashes*2))
	}
	b.WriteByte('"')
	return b.String()
}

func (p *pty) close() {
	p.ConPty.Close()
}

func (p *pty) resize(cols, rows uint16) {
	// ponytail: library takes int, cast is fine for terminal sizes
	_ = p.Resize(int(cols), int(rows))
}
