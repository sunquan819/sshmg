//go:build windows

package handler

import "testing"

func TestQuoteWindowsCommandLineArg(t *testing.T) {
	cases := map[string]string{
		`powershell.exe`:                   `powershell.exe`,
		`C:\Program Files\App\tool.exe`:    `"C:\Program Files\App\tool.exe"`,
		`try { & 'opencode' } finally { }`: `"try { & 'opencode' } finally { }"`,
		`C:\path with slash\`:              `"C:\path with slash\\"`,
	}

	for input, want := range cases {
		if got := quoteWindowsCommandLineArg(input); got != want {
			t.Fatalf("quoteWindowsCommandLineArg(%q) = %q, want %q", input, got, want)
		}
	}
}
