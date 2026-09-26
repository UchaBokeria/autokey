package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	neonPink  = lipgloss.Color("#ff2ea6")
	neonCyan  = lipgloss.Color("#00e5ff")
	neonLime  = lipgloss.Color("#b6ff00")
	warnAmber = lipgloss.Color("#ffb000")
	errRed    = lipgloss.Color("#ff3b3b")
	dimGray   = lipgloss.Color("#8a8a8a")
)

func tty() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Banner prints the neon cyberpunk header on TTY only.
func Banner() {
	if !tty() {
		return
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(neonPink).Render("AUTOKEY")
	sub := lipgloss.NewStyle().Foreground(neonCyan).Render("wildcard inbox // card pool // key pipeline")
	fmt.Printf("%s  %s\n", title, sub)
}

// Ok prints a success line.
func Ok(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(neonLime).Render("✔ " + s)
	}
	fmt.Println(s)
}

// Info prints an informational line.
func Info(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(neonCyan).Render("» " + s)
	}
	fmt.Println(s)
}

// Warn prints a warning line (also for log parity).
func Warn(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(warnAmber).Render("⚠ " + s)
	} else {
		s = "WARN: " + s
	}
	fmt.Fprintln(os.Stderr, s)
}

// Err prints an error line.
func Err(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(errRed).Render("✖ " + s)
	} else {
		s = "ERROR: " + s
	}
	fmt.Fprintln(os.Stderr, s)
}

// Dim prints a dim line.
func Dim(msg string, args ...any) {
	s := fmt.Sprintf(msg, args...)
	if tty() {
		s = lipgloss.NewStyle().Foreground(dimGray).Render(s)
	}
	fmt.Println(s)
}

// PromptSecret reads one line from the terminal without echoing it.
// Falls back to a visible prompt when stdin is not a TTY.
func PromptSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		var line string
		if _, err := fmt.Scanln(&line); err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}
