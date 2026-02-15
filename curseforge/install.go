package curseforge

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/sahilm/fuzzy"
	"github.com/spf13/viper"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"gopkg.in/dixonwille/wmenu.v4"
)

const maxCycles = 20

type installableDep struct {
	modInfo
	fileInfo modFileInfo
}

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:     "add [URL|slug|search]",
	Short:   "从CurseForge URL、slug、ID或搜索添加项目",
	Aliases: []string{"install", "get"},
	Args:    cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
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
		mcVersions, err := pack.GetSupportedMCVersions()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		primaryMCVersion, err := pack.GetMCVersion()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		game := gameFlag
		category := categoryFlag
		var modID, fileID uint32
		var slug string

		// If mod/file IDs are provided in command line, use those
		if fileIDFlag != 0 {
			fileID = fileIDFlag
		}
		if addonIDFlag != 0 {
			modID = addonIDFlag
		}

		if (len(args) == 0 || len(args[0]) == 0) && modID == 0 {
			fmt.Println("必须指定一个项目；使用ID标志，或直接传递URL、slug或搜索词。")
			os.Exit(1)
		}
		if modID == 0 && len(args) == 1 {
			parsedGame, parsedCategory, parsedSlug, parsedFileID, err := parseSlugOrUrl(args[0])
			if err != nil {
				fmt.Printf("解析URL失败：%v\n", err)
				os.Exit(1)
			}

			if parsedGame != "" {
				game = parsedGame
			}
			if parsedCategory != "" {
				category = parsedCategory
			}
			if parsedSlug != "" {
				slug = parsedSlug
			}
			if parsedFileID != 0 {
				fileID = parsedFileID
			}
		}

		modInfoObtained := false
		var modInfoData modInfo

		if modID == 0 {
			var cancelled bool
			if slug == "" {
				searchTerm := strings.Join(args, " ")
				cancelled, modInfoData = searchCurseforgeInternal(searchTerm, false, game, category, mcVersions, getSearchLoaderType(pack))
			} else {
				cancelled, modInfoData = searchCurseforgeInternal(slug, true, game, category, mcVersions, getSearchLoaderType(pack))
			}
			if cancelled {
				return
			}
			modID = modInfoData.ID
			modInfoObtained = true
		}

		if modID == 0 {
			fmt.Println("未找到任何项目！")
			os.Exit(1)
		}

		if !modInfoObtained {
			modInfoData, err = cfDefaultClient.getModInfo(modID)
			if err != nil {
				fmt.Printf("获取项目信息失败：%v\n", err)
				os.Exit(1)
			}
		}

		var fileInfoData modFileInfo
		fileInfoData, err = getLatestFile(modInfoData, mcVersions, fileID, pack.GetCompatibleLoaders())
		if err != nil {
			fmt.Printf("获取项目文件失败：%v\n", err)
			os.Exit(1)
		}

		if len(fileInfoData.Dependencies) > 0 {
			isQuilt := slices.Contains(pack.GetCompatibleLoaders(), "quilt")

			var depsInstallable []installableDep
			var depIDPendingQueue []uint32
			for _, dep := range fileInfoData.Dependencies {
				if dep.Type == dependencyTypeRequired {
					depIDPendingQueue = append(depIDPendingQueue, mapDepOverride(dep.ModID, isQuilt, primaryMCVersion))
				}
			}

			if len(depIDPendingQueue) > 0 {
				fmt.Println("正在查找依赖项...")

				cycles := 0
				var installedIDList []uint32
				for len(depIDPendingQueue) > 0 && cycles < maxCycles {
					if installedIDList == nil {
						// 获取所有模组的modids
						mods, err := index.LoadAllMods()
						if err != nil {
							fmt.Printf("无法确定现有项目：%v\n", err)
						} else {
							for _, mod := range mods {
								data, ok := mod.GetParsedUpdateData("curseforge")
								if ok {
									updateData, ok := data.(cfUpdateData)
									if ok {
										if updateData.ProjectID > 0 {
											installedIDList = append(installedIDList, updateData.ProjectID)
										}
									}
								}
							}
						}
					}

					// Remove installed IDs from dep queue
					i := 0
					for _, id := range depIDPendingQueue {
						contains := slices.Contains(installedIDList, id)
						for _, data := range depsInstallable {
							if id == data.ID {
								contains = true
								break
							}
						}
						if !contains {
							depIDPendingQueue[i] = id
							i++
						}
					}
					depIDPendingQueue = depIDPendingQueue[:i]

					if len(depIDPendingQueue) == 0 {
						break
					}

					depInfoData, err := cfDefaultClient.getModInfoMultiple(depIDPendingQueue)
					if err != nil {
						fmt.Printf("检索依赖数据时出错：%s\n", err.Error())
					}
					depIDPendingQueue = depIDPendingQueue[:0]

					for _, currData := range depInfoData {
						depFileInfo, err := getLatestFile(currData, mcVersions, 0, pack.GetCompatibleLoaders())
						if err != nil {
							fmt.Printf("检索依赖数据时出错：%s\n", err.Error())
							continue
						}

						for _, dep := range depFileInfo.Dependencies {
							if dep.Type == dependencyTypeRequired {
								depIDPendingQueue = append(depIDPendingQueue, mapDepOverride(dep.ModID, isQuilt, primaryMCVersion))
							}
						}

						depsInstallable = append(depsInstallable, installableDep{
							currData, depFileInfo,
						})
					}

					cycles++
				}
				if cycles >= maxCycles {
					fmt.Println("依赖项递归太深！尝试增加maxCycles。")
					os.Exit(1)
				}

				if len(depsInstallable) > 0 {
					fmt.Println("找到依赖项：")
					for _, v := range depsInstallable {
						fmt.Println(v.Name)
					}

					if cmdshared.PromptYesNo("要添加它们吗？[Y/n]: ") {
						for _, v := range depsInstallable {
							err = createModFile(v.modInfo, v.fileInfo, &index, false)
							if err != nil {
								fmt.Println(err)
								os.Exit(1)
							}
							fmt.Printf("依赖项 \"%s\" 成功添加！（%s）\n", v.modInfo.Name, v.fileInfo.FileName)
						}
					}
				} else {
					fmt.Println("所有依赖项都已添加！")
				}
			}
		}

		err = createModFile(modInfoData, fileInfoData, &index, false)
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

		fmt.Printf("项目 \"%s\" 成功添加！（%s）\n", modInfoData.Name, fileInfoData.FileName)
	},
}

// Used to implement interface for fuzzy matching
type modResultsList []modInfo

func (r modResultsList) String(i int) string {
	return r[i].Name
}

func (r modResultsList) Len() int {
	return len(r)
}

func searchCurseforgeInternal(searchTerm string, isSlug bool, game string, category string, mcVersions []string, searchLoaderType modloaderType) (bool, modInfo) {
	if isSlug {
		fmt.Println("正在查找CurseForge slug...")
	} else {
		fmt.Println("正在搜索CurseForge...")
	}

	var gameID, categoryID, classID uint32
	if game == "minecraft" {
		gameID = 432
	}
	if category == "mc-mods" {
		classID = 6
	}
	if gameID == 0 {
		games, err := cfDefaultClient.getGames()
		if err != nil {
			fmt.Printf("Failed to lookup game %s: %v\n", game, err)
			os.Exit(1)
		}
		for _, v := range games {
			if v.Slug == game {
				if v.Status != gameStatusLive {
					fmt.Printf("查找游戏 %s 失败：所选游戏未上线！\n", game)
					os.Exit(1)
				}
				if v.APIStatus != gameApiStatusPublic {
					fmt.Printf("查找游戏 %s 失败：所选游戏没有公共API！\n", game)
					os.Exit(1)
				}
				gameID = v.ID
				break
			}
		}
		if gameID == 0 {
			fmt.Printf("查找失败：找不到游戏 %s！\n", game)
			os.Exit(1)
		}
	}
		if categoryID == 0 && classID == 0 && category != "" {
			categories, err := cfDefaultClient.getCategories(gameID)
			if err != nil {
				fmt.Printf("查找类别失败：%v\n", err)
			os.Exit(1)
		}
		for _, v := range categories {
			if v.Slug == category {
				if v.IsClass {
					classID = v.ID
				} else {
					classID = v.ClassID
					categoryID = v.ID
				}
				break
			}
		}
		if categoryID == 0 && classID == 0 {
			fmt.Printf("查找失败：找不到类别 %s！\n", category)
			os.Exit(1)
		}
	}

	// If there are more than one acceptable version, we shouldn't filter by game version at all (as we can't filter by multiple)
	filterGameVersion := ""
	if len(mcVersions) == 1 {
		filterGameVersion = getCurseforgeVersion(mcVersions[0])
	}
	var search, slug string
	if isSlug {
		slug = searchTerm
	} else {
		search = searchTerm
	}
	results, err := cfDefaultClient.getSearch(search, slug, gameID, classID, categoryID, filterGameVersion, searchLoaderType)
	if err != nil {
		fmt.Printf("搜索项目失败：%v\n", err)
		os.Exit(1)
	}
	if len(results) == 0 {
		fmt.Println("未找到任何项目！")
		os.Exit(1)
		return false, modInfo{}
	} else if len(results) == 1 {
		return false, results[0]
	} else {
		// Fuzzy search on results list
		fuzzySearchResults := fuzzy.FindFrom(searchTerm, modResultsList(results))

		if viper.GetBool("non-interactive") {
			if len(fuzzySearchResults) > 0 {
				return false, results[fuzzySearchResults[0].Index]
			}
			return false, results[0]
		}

		menu := wmenu.NewMenu("选择一个数字：")

		menu.Option("取消", nil, false, nil)
		if len(fuzzySearchResults) == 0 {
			for i, v := range results {
				menu.Option(v.Name+" ("+v.Summary+")", v, i == 0, nil)
			}
		} else {
			for i, v := range fuzzySearchResults {
				menu.Option(results[v.Index].Name+" ("+results[v.Index].Summary+")", results[v.Index], i == 0, nil)
			}
		}

		var modInfoData modInfo
		var cancelled bool
		menu.Action(func(menuRes []wmenu.Opt) error {
			if len(menuRes) != 1 || menuRes[0].Value == nil {
				fmt.Println("已取消！")
				cancelled = true
				return nil
			}

			// Why is variable shadowing a thing!!!!
			var ok bool
			modInfoData, ok = menuRes[0].Value.(modInfo)
			if !ok {
				return errors.New("从wmenu转换接口时出错")
			}
			return nil
		})
		err = menu.Run()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if cancelled {
			return true, modInfo{}
		}
		return false, modInfoData
	}
}

func getLatestFile(modInfoData modInfo, mcVersions []string, fileID uint32, packLoaders []string) (modFileInfo, error) {
	if fileID == 0 {
		if len(modInfoData.LatestFiles) == 0 && len(modInfoData.GameVersionLatestFiles) == 0 {
			return modFileInfo{}, fmt.Errorf("addon %d 没有文件", modInfoData.ID)
		}

		var fileInfoData *modFileInfo
		fileID, fileInfoData, _ = findLatestFile(modInfoData, mcVersions, packLoaders)
		if fileInfoData != nil {
			return *fileInfoData, nil
		}

		// Possible to reach this point without obtaining file info; particularly from GameVersionLatestFiles
		if fileID == 0 {
			return modFileInfo{}, errors.New("mod不适用于配置的Minecraft版本（使用'packwiz settings acceptable-versions'命令以接受更多版本）或加载器")
		}
	}

	fileInfoData, err := cfDefaultClient.getFileInfo(modInfoData.ID, fileID)
	if err != nil {
		return modFileInfo{}, err
	}
	return fileInfoData, nil
}

var addonIDFlag uint32
var fileIDFlag uint32

var gameFlag string
var categoryFlag string

func init() {
	curseforgeCmd.AddCommand(installCmd)

	installCmd.Flags().Uint32Var(&addonIDFlag, "addon-id", 0, "要使用的CurseForge项目ID")
	installCmd.Flags().Uint32Var(&fileIDFlag, "file-id", 0, "要使用的CurseForge文件ID")
	installCmd.Flags().StringVar(&gameFlag, "game", "minecraft", "要从中添加文件的游戏（slug，如存储在URL中）；URL中的游戏优先")
	installCmd.Flags().StringVar(&categoryFlag, "category", "", "要从中添加文件的类别（slug，如存储在URL中）；URL中的类别优先")
}
