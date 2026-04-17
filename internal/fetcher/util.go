package fetcher

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func runCommand(command string, out any) error {
	c := strings.Split(command, " ")
	cmd := exec.Command(c[0], c[1:]...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run '%s': %w", command, err)
	}

	if err := json.Unmarshal(output, &out); err != nil {
		return fmt.Errorf("failed to parse output of '%s': %w", command, err)
	}

	return nil
}
