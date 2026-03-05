package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/server"
	"github.com/tgeorge06/atlaskb/web"
)

var servePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the AtlasKB runtime",
	Long:  "Start the AtlasKB runtime: web dashboard plus MCP over HTTP at /mcp.",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 3000, "port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	port := servePort
	if !cmd.Flags().Changed("port") && cfg.Server.Port > 0 {
		port = cfg.Server.Port
	}

	embedClient := embeddings.NewOpenAIClient(cfg.Embeddings.BaseURL, cfg.Embeddings.APIKey)
	llmClient := llm.NewOpenAIClient(cfg.LLM.BaseURL, cfg.LLM.APIKey)

	webFS := web.DistFS()

	srv := server.New(pool, embedClient, llmClient, cfg, webFS, cfgPath)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("AtlasKB dashboard running at http://localhost%s\n", addr)
	fmt.Printf("AtlasKB MCP endpoint running at http://localhost%s/mcp\n", addr)
	return http.ListenAndServe(addr, srv)
}
