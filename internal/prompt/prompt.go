// Package prompt provides user interaction utilities.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Confirm prompts the user with a yes/no question and returns true if they confirm.
func Confirm(message string) bool {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("  ? %s [y/N]: ", message)

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
