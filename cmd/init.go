package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fatih/camelcase"
	"github.com/igorsobreira/titlecase"
	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 packwiz 模组包",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		_, err := os.Stat(viper.GetString("pack-file"))
		if err == nil && !viper.GetBool("init.reinit") {
			fmt.Println("模组包元数据文件已存在，使用 -r 覆盖！")
			os.Exit(1)
		} else if err != nil && !os.IsNotExist(err) {
			fmt.Printf("检查模组包文件时出错：%s\n", err)
			os.Exit(1)
		}

		name, err := cmd.Flags().GetString("name")
		if err != nil || len(name) == 0 {
			// Get current file directory name
			wd, err := os.Getwd()
			directoryName := "."
			if err == nil {
				directoryName = filepath.Base(wd)
			}
			if directoryName != "." && len(directoryName) > 0 {
				// Turn directory name into a space-seperated proper name
				name = titlecase.Title(strings.ReplaceAll(strings.ReplaceAll(strings.Join(camelcase.Split(directoryName), " "), " - ", " "), " _ ", " "))
				name = initReadValue("模组包名称 ["+name+"]：", name)
			} else {
				name = initReadValue("模组包名称：", "")
			}
		}

		author, err := cmd.Flags().GetString("author")
		if err != nil || len(author) == 0 {
			author = initReadValue("作者：", "")
		}

		version, err := cmd.Flags().GetString("version")
		if err != nil || len(version) == 0 {
			version = initReadValue("版本 [1.0.0]：", "1.0.0")
		}

		mcVersions, err := cmdshared.GetValidMCVersions()
		if err != nil {
			fmt.Printf("获取最新 Minecraft 版本失败：%s\n", err)
			os.Exit(1)
		}

		mcVersion := viper.GetString("init.mc-version")
		if len(mcVersion) == 0 {
			var latestVersion string
			if viper.GetBool("init.snapshot") {
				latestVersion = mcVersions.Latest.Snapshot
			} else {
				latestVersion = mcVersions.Latest.Release
			}
			if viper.GetBool("init.latest") {
				mcVersion = latestVersion
			} else {
				mcVersion = initReadValue("Minecraft 版本 ["+latestVersion+"]：", latestVersion)
			}
		}
		mcVersions.CheckValid(mcVersion)

		modLoaderName := strings.ToLower(viper.GetString("init.modloader"))
		if len(modLoaderName) == 0 {
			modLoaderName = strings.ToLower(initReadValue("模组加载器 [quilt]：", "quilt"))
		}

		loader, ok := core.ModLoaders[modLoaderName]
		modLoaderVersions := make(map[string]string)
		if modLoaderName != "none" {
			if ok {
				versions, latestVersion, err := loader.VersionListGetter(mcVersion)
				if err != nil {
					fmt.Printf("加载版本时出错：%s\n", err)
					os.Exit(1)
				}
				componentVersion := viper.GetString("init." + loader.Name + "-version")
				if len(componentVersion) == 0 {
					if viper.GetBool("init." + loader.Name + "-latest") {
						componentVersion = latestVersion
					} else {
						componentVersion = initReadValue(loader.FriendlyName+" 版本 ["+latestVersion+"]：", latestVersion)
					}
				}
				v := componentVersion
				// Forge uses a format where they prefix their version with their supported minecraft version. NeoForge
				// did this too, but only during the 1.20.1 days, they've since switched formats.
				if loader.Name == "forge" || (loader.Name == "neoforge" && mcVersion == "1.20.1") {
					v = cmdshared.GetRawForgeVersion(componentVersion)
				}
				if !slices.Contains(versions, v) {
					fmt.Println("找不到指定的 " + loader.FriendlyName + " 版本！")
					os.Exit(1)
				}
				modLoaderVersions[loader.Name] = v
			} else {
				fmt.Println("指定的模组加载器不受支持！使用 \"none\" 指定无模组加载器，或手动配置一个。")
				fmt.Print("支持以下模组加载器：")
				keys := make([]string, len(core.ModLoaders))
				i := 0
				for k := range core.ModLoaders {
					keys[i] = k
					i++
				}
				fmt.Println(strings.Join(keys, ", "))
				os.Exit(1)
			}
		}

		indexFilePath := viper.GetString("init.index-file")
		_, err = os.Stat(indexFilePath)
		if os.IsNotExist(err) {
			// Create file
			err = os.WriteFile(indexFilePath, []byte{}, 0644)
			if err != nil {
				fmt.Printf("Error creating index file: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(indexFilePath + " 已创建！")
		} else if err != nil {
			fmt.Printf("检查索引文件时出错：%s\n", err)
			os.Exit(1)
		}

		// Create the pack
		pack := core.Pack{
			Name:       name,
			Author:     author,
			Version:    version,
			PackFormat: core.CurrentPackFormat,
			Index: struct {
				File       string `toml:"file"`
				HashFormat string `toml:"hash-format"`
				Hash       string `toml:"hash,omitempty"`
			}{
				File: indexFilePath,
			},
			Versions: map[string]string{
				"minecraft": mcVersion,
			},
		}
		if modLoaderName != "none" {
			for k, v := range modLoaderVersions {
				pack.Versions[k] = v
			}
		}

		// Refresh the index and pack
		index, err := pack.LoadIndex()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = index.Refresh()
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
		fmt.Println(viper.GetString("pack-file") + " 已创建！")
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().String("name", "", "模组包名称（省略以交互方式定义）")
	initCmd.Flags().String("author", "", "模组包作者（省略以交互方式定义）")
	initCmd.Flags().String("version", "", "模组包版本（省略以交互方式定义）")
	initCmd.Flags().String("index-file", "index.toml", "要使用的索引文件")
	_ = viper.BindPFlag("init.index-file", initCmd.Flags().Lookup("index-file"))
	initCmd.Flags().String("mc-version", "", "要使用的 Minecraft 版本（省略以交互方式定义）")
	_ = viper.BindPFlag("init.mc-version", initCmd.Flags().Lookup("mc-version"))
	initCmd.Flags().BoolP("latest", "l", false, "自动选择最新的 Minecraft 版本")
	_ = viper.BindPFlag("init.latest", initCmd.Flags().Lookup("latest"))
	initCmd.Flags().BoolP("snapshot", "s", false, "与 --latest 一起使用最新的快照版本")
	_ = viper.BindPFlag("init.snapshot", initCmd.Flags().Lookup("snapshot"))
	initCmd.Flags().BoolP("reinit", "r", false, "如果文件已存在，重新创建模组包文件而不是退出")
	_ = viper.BindPFlag("init.reinit", initCmd.Flags().Lookup("reinit"))
	initCmd.Flags().String("modloader", "", "要使用的模组加载器（省略以交互方式定义）")
	_ = viper.BindPFlag("init.modloader", initCmd.Flags().Lookup("modloader"))

	// ok this is epic
	for _, loader := range core.ModLoaders {
		initCmd.Flags().String(loader.Name+"-version", "", "要使用的 "+loader.FriendlyName+" 版本（省略以交互方式定义）")
		_ = viper.BindPFlag("init."+loader.Name+"-version", initCmd.Flags().Lookup(loader.Name+"-version"))
		initCmd.Flags().Bool(loader.Name+"-latest", false, "自动选择最新的 "+loader.FriendlyName+" 版本")
		_ = viper.BindPFlag("init."+loader.Name+"-latest", initCmd.Flags().Lookup(loader.Name+"-latest"))
	}
}

func initReadValue(prompt string, def string) string {
	fmt.Print(prompt)
	if viper.GetBool("non-interactive") {
		fmt.Printf("%s\n", def)
		return def
	}
	value, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Printf("读取输入时出错：%s\n", err)
		os.Exit(1)
	}
	// Trims both CR and LF
	value = strings.TrimSpace(strings.TrimRight(value, "\r\n"))
	if len(value) > 0 {
		return value
	}
	return def
}
