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
	Short: "Start the MCP server for Claude Code integration",
	Long:  "Runs an MCP (Model Context Protocol) server over stdio, exposing AtlasKB tools to Claude Code and other MCP clients.",
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
