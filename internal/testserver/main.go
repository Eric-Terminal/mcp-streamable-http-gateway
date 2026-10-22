package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoArgs struct {
	Message string `json:"message"`
}

type echoResult struct {
	Message string `json:"message"`
}

func main() {
	if path := os.Getenv("MCP_TEST_EXIT_FILE"); path != "" {
		defer func() {
			if err := os.WriteFile(path, []byte("已退出\n"), 0600); err != nil {
				log.Print(err)
			}
		}()
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-stdio-server",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo",
		Description: "返回输入的消息",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, echoResult, error) {
		return nil, echoResult{Message: args.Message}, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
