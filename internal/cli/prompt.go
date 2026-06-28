package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// readLine reads byte-by-byte from r until a newline is hit. This prevents
// buffering that would consume inputs intended for subsequent prompts.
func readLine(r io.Reader) (string, error) {
	var buf []byte
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			buf = append(buf, b[0])
		}
		if err != nil {
			if len(buf) > 0 && errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
	}
	line := string(buf)
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, nil
}

// parseString checks if the input is non-empty, falling back to def if input is empty.
// Returns (value, ok). If ok is false, input was empty and no default exists.
func parseString(input string, def string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		if def != "" {
			return def, true
		}
		return "", false
	}
	return input, true
}

// parseChoice parses a 1-based numeric choice. Returns the 0-based index and ok.
func parseChoice(input string, numChoices int) (int, bool) {
	input = strings.TrimSpace(input)
	val, err := strconv.Atoi(input)
	if err != nil {
		return 0, false
	}
	if val < 1 || val > numChoices {
		return 0, false
	}
	return val - 1, true
}

// parseConfirm parses y/N options. Returns the parsed boolean and ok.
func parseConfirm(input string, defYes bool) (bool, bool) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return defYes, true
	}
	if input == "y" || input == "yes" {
		return true, true
	}
	if input == "n" || input == "no" {
		return false, true
	}
	return false, false
}

// promptString displays a prompt and reads user input. If input is empty and def is provided,
// def is returned. If input is empty and no default is provided, it loops.
func promptString(prompt string, def string, r io.Reader, w io.Writer) (string, error) {
	for {
		if def != "" {
			fmt.Fprintf(w, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(w, "%s: ", prompt)
		}

		line, err := readLine(r)
		if err != nil {
			return "", err
		}

		val, ok := parseString(line, def)
		if ok {
			return val, nil
		}
	}
}

// promptSecret prompts for a secret. If r is a terminal, it reads without echoing.
func promptSecret(prompt string, r io.Reader, w io.Writer) (string, error) {
	isTerm := false
	var fd int
	if f, ok := r.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		isTerm = true
		fd = int(f.Fd())
	}

	for {
		fmt.Fprintf(w, "%s: ", prompt)

		var input string
		if isTerm {
			byteKey, err := term.ReadPassword(fd)
			if err != nil {
				return "", err
			}
			fmt.Fprintln(w)
			input = string(byteKey)
		} else {
			line, err := readLine(r)
			if err != nil {
				return "", err
			}
			input = line
		}

		input = strings.TrimSpace(input)
		if input != "" {
			return input, nil
		}
	}
}

// promptChoice presents a numbered list of choices and prompts for a selection.
func promptChoice(prompt string, choices []string, r io.Reader, w io.Writer) (int, error) {
	for {
		for i, choice := range choices {
			fmt.Fprintf(w, "%d) %s\n", i+1, choice)
		}
		fmt.Fprintf(w, "%s: ", prompt)

		line, err := readLine(r)
		if err != nil {
			return 0, err
		}

		idx, ok := parseChoice(line, len(choices))
		if ok {
			return idx, nil
		}
	}
}

// promptConfirm prompts for a yes/no confirmation.
func promptConfirm(prompt string, defYes bool, r io.Reader, w io.Writer) (bool, error) {
	for {
		if defYes {
			fmt.Fprintf(w, "%s [Y/n]: ", prompt)
		} else {
			fmt.Fprintf(w, "%s [y/N]: ", prompt)
		}

		line, err := readLine(r)
		if err != nil {
			return false, err
		}

		val, ok := parseConfirm(line, defYes)
		if ok {
			return val, nil
		}
	}
}
