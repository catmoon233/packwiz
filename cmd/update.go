package cmd

import (
	"fmt"
	"os"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// UpdateCmd represents the update command
var UpdateCmd = &cobra.Command{
	Use:     "update [name]",
	Short:   "更新模组包中的外部文件（或所有外部文件）",
	Aliases: []string{"upgrade"},
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: --check flag?
		// TODO: specify multiple files to update at once?

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

		var singleUpdatedName string
		if viper.GetBool("update.all") {
			filesWithUpdater := make(map[string][]*core.Mod)
			fmt.Println("正在读取元数据文件...")
			mods, err := index.LoadAllMods()
			if err != nil {
				fmt.Printf("更新所有文件失败：%v\n", err)
				os.Exit(1)
			}
			for _, modData := range mods {
				updaterFound := false
				for k := range modData.Update {
					slice, ok := filesWithUpdater[k]
					if !ok {
						_, ok = core.Updaters[k]
						if !ok {
							continue
						}
						slice = []*core.Mod{}
					}
					updaterFound = true
					filesWithUpdater[k] = append(slice, modData)
				}
				if !updaterFound {
					fmt.Printf("找不到 \"%s\" 的支持更新系统。\n", modData.Name)
				}
			}

			fmt.Println("正在检查更新...")
			updatesFound := false
			updatableFiles := make(map[string][]*core.Mod)
			updaterCachedStateMap := make(map[string][]interface{})
			for k, v := range filesWithUpdater {
				checks, err := core.Updaters[k].CheckUpdate(v, pack)
				if err != nil {
					// TODO: do we return err code 1?
					fmt.Printf("检查 %s 的更新时失败：%s\n", k, err.Error())
					continue
				}
				for i, check := range checks {
					if check.Error != nil {
						// TODO: do we return err code 1?
						fmt.Printf("检查 %s 的更新时失败：%s\n", v[i].Name, check.Error.Error())
						continue
					}
					if check.UpdateAvailable {
						if v[i].Pin {
							fmt.Printf("已跳过固定的模组 %s 的更新\n", v[i].Name)
							continue
						}

						if !updatesFound {
							fmt.Println("发现更新：")
							updatesFound = true
						}
						fmt.Printf("%s: %s\n", v[i].Name, check.UpdateString)
						updatableFiles[k] = append(updatableFiles[k], v[i])
						updaterCachedStateMap[k] = append(updaterCachedStateMap[k], check.CachedState)
					}
				}
			}

			if !updatesFound {
				fmt.Println("所有文件都是最新的！")
				return
			}

			if !cmdshared.PromptYesNo("您要更新吗？[Y/n]：") {
				fmt.Println("已取消！")
				return
			}

			for k, v := range updatableFiles {
				err := core.Updaters[k].DoUpdate(v, updaterCachedStateMap[k])
				if err != nil {
					// TODO: do we return err code 1?
					fmt.Println(err.Error())
					continue
				}
				for _, modData := range v {
					format, hash, err := modData.Write()
					if err != nil {
						fmt.Println(err.Error())
						continue
					}
					err = index.RefreshFileWithHash(modData.GetFilePath(), format, hash, true)
					if err != nil {
						fmt.Println(err.Error())
						continue
					}
				}
			}
		} else {
			if len(args) < 1 || len(args[0]) == 0 {
				fmt.Println("必须指定有效文件，或使用 --all 标志！")
				os.Exit(1)
			}
			modPath, ok := index.FindMod(args[0])
			if !ok {
				fmt.Println("找不到此文件；请确保您已运行 packwiz refresh 并使用 .pw.toml 文件的名称（默认为项目 slug）")
				os.Exit(1)
			}
			modData, err := core.LoadMod(modPath)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if modData.Pin {
				fmt.Println("版本已固定；运行 unpin 命令以允许更新")
				os.Exit(1)
			}
			singleUpdatedName = modData.Name
			updaterFound := false
			for k := range modData.Update {
				updater, ok := core.Updaters[k]
				if !ok {
					continue
				}
				updaterFound = true

				check, err := updater.CheckUpdate([]*core.Mod{&modData}, pack)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
				if len(check) != 1 {
					fmt.Println("无效的更新检查响应")
					os.Exit(1)
				}

				if check[0].UpdateAvailable {
					fmt.Printf("可用更新：%s\n", check[0].UpdateString)

					err = updater.DoUpdate([]*core.Mod{&modData}, []interface{}{check[0].CachedState})
					if err != nil {
						fmt.Println(err)
						os.Exit(1)
					}

					format, hash, err := modData.Write()
					if err != nil {
						fmt.Println(err)
						os.Exit(1)
					}
					err = index.RefreshFileWithHash(modPath, format, hash, true)
					if err != nil {
						fmt.Println(err)
						os.Exit(1)
					}
				} else {
					fmt.Printf("\"%s\" 已经是最新版本！\n", modData.Name)
					return
				}

				break
			}
			if !updaterFound {
				// TODO: use file name instead of Name when len(Name) == 0 in all places?
				fmt.Println("找不到 \"" + modData.Name + "\" 的支持更新系统。")
				os.Exit(1)
			}
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
		if viper.GetBool("update.all") {
			fmt.Println("文件已更新！")
		} else {
			fmt.Printf("\"%s\" 已更新！\n", singleUpdatedName)
		}
	},
}

func init() {
	rootCmd.AddCommand(UpdateCmd)

	UpdateCmd.Flags().BoolP("all", "a", false, "更新所有外部文件")
	_ = viper.BindPFlag("update.all", UpdateCmd.Flags().Lookup("all"))
}
