package cmd

import (
	"fmt"
	"os"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
)

// removeCmd represents the remove command
var removeCmd = &cobra.Command{
	Use:     "remove",
	Short:   "从模组包中移除外部文件；相当于手动删除文件并运行 packwiz refresh",
	Aliases: []string{"delete", "uninstall", "rm"},
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("正在加载模组包...")
		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		index, err := pack.LoadIndex()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		resolvedMod, ok := index.FindMod(args[0])
		if !ok {
			fmt.Println("找不到此文件；请确保您已运行 packwiz refresh 并使用 .pw.toml 文件的名称（默认为项目 slug）")
			os.Exit(1)
		}
		err = os.Remove(resolvedMod)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("正在从索引中移除文件...")
		err = index.RemoveFile(resolvedMod)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = index.Write()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = pack.UpdateIndexHash()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = pack.Write()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Printf("%s 已成功移除！\n", args[0])
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
