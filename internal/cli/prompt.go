package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Neha611/commhub/internal/safe"
	"golang.org/x/term"
)

// Passphrase reads without echo. It is the PassphraseFunc for the sealed-file
// secrets backend.
func Passphrase(prompt string, confirm bool) (safe.Secret, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return safe.Secret{}, errors.New("a passphrase is required but stdin is not a terminal")
	}
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return safe.Secret{}, err
	}
	if len(b) == 0 {
		return safe.Secret{}, errors.New("empty passphrase")
	}
	if confirm {
		fmt.Fprint(os.Stderr, "Confirm: ")
		b2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return safe.Secret{}, err
		}
		if string(b) != string(b2) {
			return safe.Secret{}, errors.New("passphrases did not match")
		}
	}
	return safe.NewSecret(string(b)), nil
}

func Confirm(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
