package cmd

import (
	"fmt"
	"os"

	"github.com/packwiz/packwiz/cmdshared"

	"slices"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
)

// rehashCmd represents the rehash command
var rehashCmd = &cobra.Command{
	Use:   "rehash [hash format]",
	Short: "将所有哈希迁移到特定格式",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		// Load pack
		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Load index
		index, err := pack.LoadIndex()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Load mods
		mods, err := index.LoadAllMods()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if !slices.Contains([]string{"sha1", "sha512", "sha256"}, args[0]) {
			fmt.Printf("不支持的哈希格式 '%s'\n", args[0])
			os.Exit(1)
		}

		session, err := core.CreateDownloadSession(mods, []string{args[0]})
		if err != nil {
			fmt.Printf("获取外部文件时出错：%v\n", err)
			os.Exit(1)
		}

		cmdshared.ListManualDownloads(session)

		for dl := range session.StartDownloads() {
			if dl.Error != nil {
				fmt.Printf("获取 %s 时出错：%v\n", dl.Mod.Name, dl.Error)
			} else {
				dl.Mod.Download.HashFormat = args[0]
				dl.Mod.Download.Hash = dl.Hashes[args[0]]
				_, _, err := dl.Mod.Write()
				if err != nil {
					fmt.Printf("保存模组 %s 时出错：%v\n", dl.Mod.Name, err)
					os.Exit(1)
				}
			}
			// TODO pass the hash to index instead of recomputing from scratch
		}

		err = session.SaveIndex()
		if err != nil {
			fmt.Printf("保存缓存索引时出错：%v\n", err)
			os.Exit(1)
		}

		err = index.Refresh()
		if err != nil {
			fmt.Printf("刷新索引时出错：%v\n", err)
			os.Exit(1)
		}

		err = index.Write()
		if err != nil {
			fmt.Printf("写入索引时出错：%v\n", err)
			os.Exit(1)
		}

		err = pack.UpdateIndexHash()
		if err != nil {
			fmt.Printf("更新索引哈希时出错：%v\n", err)
			os.Exit(1)
		}

		err = pack.Write()
		if err != nil {
			fmt.Printf("写入模组包时出错：%v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(rehashCmd)
}
