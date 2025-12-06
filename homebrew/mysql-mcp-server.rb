class MysqlMcpServer < Formula
  desc "MySQL MCP Server - Model Context Protocol server for MySQL databases"
  homepage "https://github.com/askdba/mysql-mcp-server"
  version "1.1.0"
  license "Apache-2.0"

  on_macos do
    on_intel do
      url "https://github.com/askdba/mysql-mcp-server/releases/download/v1.1.0/mysql-mcp-server_1.1.0_darwin_amd64.tar.gz"
      sha256 "b27c0944c4b5fd1a11bddc7c29f04dac3b62cbfe004572756acfb12f97acb240"
    end
    on_arm do
      url "https://github.com/askdba/mysql-mcp-server/releases/download/v1.1.0/mysql-mcp-server_1.1.0_darwin_arm64.tar.gz"
      sha256 "182a11c03f1a8fb8f74f1e0a8fdc49545f664fa58794c83e18f68f92f153172b"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/askdba/mysql-mcp-server/releases/download/v1.1.0/mysql-mcp-server_1.1.0_linux_amd64.tar.gz"
      sha256 "b63da1d29652bb7b64a0e85cd96d56c4ba1a27cace2ee60349d28272df1335ac"
    end
    on_arm do
      url "https://github.com/askdba/mysql-mcp-server/releases/download/v1.1.0/mysql-mcp-server_1.1.0_linux_arm64.tar.gz"
      sha256 "2bb22a0fdd4e7623ebbbe7d02df84780bfd3f12517d611d32769b2e1e3e8140e"
    end
  end

  def install
    bin.install "mysql-mcp-server"
  end

  def caveats
    <<~EOS
      To use mysql-mcp-server with Claude Desktop, add to your config:

        {
          "mcpServers": {
            "mysql": {
              "command": "#{opt_bin}/mysql-mcp-server",
              "env": {
                "MYSQL_DSN": "user:password@tcp(localhost:3306)/database"
              }
            }
          }
        }

      Config location:
        macOS: ~/Library/Application Support/Claude/claude_desktop_config.json
        Linux: ~/.config/Claude/claude_desktop_config.json
    EOS
  end

  test do
    # Basic test - server should fail without MYSQL_DSN but show proper error
    output = shell_output("#{bin}/mysql-mcp-server 2>&1", 1)
    assert_match(/MYSQL_DSN|config error/i, output)
  end
end

