// Command tefrun runs MCP tool-selection evals against a local mcp-tef server.
package main

import (
	"os"

	"github.com/Namit2003/mcp-eval/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
