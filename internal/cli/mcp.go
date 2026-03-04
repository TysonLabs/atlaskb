package cli

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/mcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server over stdio (legacy mode)",
	Long:  "Runs MCP over stdio for clients that require stdio transport. For combined runtime, use `atlaskb` or `atlaskb serve` and connect to /mcp over HTTP.",
	RunE:  runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	// Redirect all logging to stderr to keep stdout clean for JSON-RPC.
	log.SetOutput(os.Stderr)

	embedClient := embeddings.NewOpenAIClient(cfg.Embeddings.BaseURL, cfg.Embeddings.APIKey)
	srv := mcp.NewServer(pool, embedClient)
	return srv.Run(cmd.Context())
}
