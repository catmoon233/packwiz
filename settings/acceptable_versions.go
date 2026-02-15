package settings

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/unascribed/FlexVer/go/flexver"
)

var acceptableVersionsCommand = &cobra.Command{
	Use:     "acceptable-versions",
	Short:   "管理 pack 的可接受 Minecraft 版本。这必须是一个逗号分隔的 Minecraft 版本列表，例如 1.16.3,1.16.4,1.16.5",
	Aliases: []string{"av"},
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		modpack, err := core.LoadPack()
		if err != nil {
			// Check if it's a no such file or directory error
			if os.IsNotExist(err) {
				fmt.Println("未找到 pack.toml 文件，请运行 'packwiz init' 创建一个！")
				os.Exit(1)
			}
			fmt.Printf("加载 pack 出错：%s\n", err)
			os.Exit(1)
		}
		var currentVersions []string
		// Check if they have no options whatsoever
		if modpack.Options == nil {
			// Initialize the options
			modpack.Options = make(map[string]interface{})
		}
		// Check if the acceptable-game-versions is nil, which would mean their pack.toml doesn't have it set yet
		if modpack.Options["acceptable-game-versions"] != nil {
			// Convert the interface{} to a string slice
			for _, v := range modpack.Options["acceptable-game-versions"].([]interface{}) {
				currentVersions = append(currentVersions, v.(string))
			}
		}
		// Check our flags to see if we're adding or removing
		if flagAdd {
			acceptableVersion := args[0]
			// Check if the version is already in the list
			if slices.Contains(currentVersions, acceptableVersion) {
				fmt.Printf("版本 %s 已在您的可接受版本列表中！\n", acceptableVersion)
				os.Exit(1)
			}
			// Add the version to the list and re-sort it
			currentVersions = append(currentVersions, acceptableVersion)
			flexver.VersionSlice(currentVersions).Sort()
			// Set the new list
			modpack.Options["acceptable-game-versions"] = currentVersions
			// Save the pack
			err = modpack.Write()
			if err != nil {
				fmt.Printf("写入 pack 出错：%s\n", err)
				os.Exit(1)
			}
			// Print success message
			prettyList := strings.Join(currentVersions, ", ")
			prettyList += ", " + modpack.Versions["minecraft"]
			fmt.Printf("已将 %s 添加到可接受版本列表，现在为 %s\n", acceptableVersion, prettyList)
		} else if flagRemove {
			acceptableVersion := args[0]
			// Check if the version is in the list
			if !slices.Contains(currentVersions, acceptableVersion) {
				fmt.Printf("版本 %s 不在您的可接受版本列表中！\n", acceptableVersion)
				os.Exit(1)
			}
			// Remove the version from the list
			i := slices.Index(currentVersions, acceptableVersion)
			currentVersions = slices.Delete(currentVersions, i, i+1)
			// Sort it just in case it's out of order
			flexver.VersionSlice(currentVersions).Sort()
			// Set the new list
			modpack.Options["acceptable-game-versions"] = currentVersions
			// Save the pack
			err = modpack.Write()
			if err != nil {
				fmt.Printf("写入 pack 出错：%s\n", err)
				os.Exit(1)
			}
			// Print success message
			prettyList := strings.Join(currentVersions, ", ")
			prettyList += ", " + modpack.Versions["minecraft"]
			fmt.Printf("已从可接受版本列表中移除 %s，现在为 %s\n", acceptableVersion, prettyList)
		} else {
			// Overwriting
			acceptableVersions := args[0]
			acceptableVersionsList := strings.Split(acceptableVersions, ",")
			// Dedupe the list
			acceptableVersionsDeduped := []string(nil)
			for i, v := range acceptableVersionsList {
				if !slices.Contains(acceptableVersionsList[i+1:], v) {
					acceptableVersionsDeduped = append(acceptableVersionsDeduped, v)
				}
			}
			// Check if the list of versions is out of order, lowest to highest, and inform the user if it is
			// Compare the versions one by one to the next one, if the next one is lower, then it's out of order
			// If it's only 1 element long, then it's already sorted
			if len(acceptableVersionsDeduped) > 1 {
				for i, v := range acceptableVersionsDeduped {
					if i+1 < len(acceptableVersionsDeduped) && flexver.Less(acceptableVersionsDeduped[i+1], v) {
						fmt.Printf("警告：您的可接受版本列表顺序不对。")
						// Give a do you mean example
						// Clone the list
						acceptableVersionsDedupedClone := make([]string, len(acceptableVersionsDeduped))
						copy(acceptableVersionsDedupedClone, acceptableVersionsDeduped)
						flexver.VersionSlice(acceptableVersionsDedupedClone).Sort()
						fmt.Printf("您的意思是 %s 吗？\n", strings.Join(acceptableVersionsDedupedClone, ", "))
						if cmdshared.PromptYesNo("您想自动修复这个问题吗？[Y/n] ") {
							// If yes we'll just set the list to the sorted one
							acceptableVersionsDeduped = acceptableVersionsDedupedClone
							break
						} else {
							// If no we'll just continue
							break
						}
					}
				}
			}
			modpack.Options["acceptable-game-versions"] = acceptableVersionsDeduped
			err = modpack.Write()
			if err != nil {
				fmt.Printf("写入 pack 出错：%s\n", err)
				os.Exit(1)
			}
			// Print success message
			prettyList := strings.Join(acceptableVersionsDeduped, ", ")
			prettyList += ", " + modpack.Versions["minecraft"]
			fmt.Printf("已将可接受版本设置为 %s\n", prettyList)
		}
	},
}

var flagAdd bool
var flagRemove bool

func init() {
	settingsCmd.AddCommand(acceptableVersionsCommand)

	// Add and remove flags for adding or removing specific versions
	acceptableVersionsCommand.Flags().BoolVarP(&flagAdd, "add", "a", false, "添加一个版本到列表")
	acceptableVersionsCommand.Flags().BoolVarP(&flagRemove, "remove", "r", false, "从列表中移除一个版本")
}
