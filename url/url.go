package url

import (
	"github.com/packwiz/packwiz/cmd"
	"github.com/spf13/cobra"
)

var urlCmd = &cobra.Command{
	Use:   "url",
	Short: "从直接下载链接添加外部文件，用于packwiz不直接支持的网站",
}

func init() {
	cmd.Add(urlCmd)
}
