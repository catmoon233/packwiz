package modrinth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	modrinthApi "codeberg.org/jmansfield/go-modrinth/modrinth"
	"github.com/packwiz/packwiz/cmdshared"
	"github.com/spf13/viper"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"gopkg.in/dixonwille/wmenu.v4"
)

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:     "add [URL|slug|search]",
	Short:   "从Modrinth URL、slug/项目ID或搜索添加项目",
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

		// If project/version IDs/version file name is provided in command line, use those
		var projectID, versionID, versionFilename string
		if projectIDFlag != "" {
			projectID = projectIDFlag
			if len(args) != 0 {
				fmt.Println("--project-id cannot be used with a separately specified URL/slug/search term")
				os.Exit(1)
			}
		}
		if versionIDFlag != "" {
			versionID = versionIDFlag
			if len(args) != 0 {
				fmt.Println("--version-id cannot be used with a separately specified URL/slug/search term")
				os.Exit(1)
			}
		}
		if versionFilenameFlag != "" {
			versionFilename = versionFilenameFlag
		}

		if (len(args) == 0 || len(args[0]) == 0) && projectID == "" {
			fmt.Println("You must specify a project; with the ID flags, or by passing a URL, slug or search term directly.")
			os.Exit(1)
		}

		var version string
		var parsedSlug bool
		if projectID == "" && versionID == "" && len(args) == 1 {
			// Try interpreting the argument as a slug/project ID, or project/version/CDN URL
			parsedSlug, err = parseSlugOrUrl(args[0], &projectID, &version, &versionID, &versionFilename)
			if err != nil {
				fmt.Printf("解析URL失败：%v\n", err)
				os.Exit(1)
			}
		}

		// Got version ID; install using this ID
		if versionID != "" {
			err = installVersionById(versionID, versionFilename, pack, &index)
			if err != nil {
				fmt.Printf("添加项目失败：%s\n", err)
				os.Exit(1)
			}
			return
		}

		// Look up project ID
		if projectID != "" {
			// Modrinth transparently handles slugs/project IDs in their API; we don't have to detect which one it is.
			var project *modrinthApi.Project
			project, err = mrDefaultClient.Projects.Get(projectID)
			if err == nil {
				// We found a project with that id/slug
				if version != "" {
					// Try to look up version number
					versionData, err := resolveVersion(project, version)
					if err != nil {
						fmt.Printf("添加项目失败：%s\n", err)
						os.Exit(1)
					}
					err = installVersion(project, versionData, versionFilename, pack, &index)
					if err != nil {
						fmt.Printf("Failed to add project: %s\n", err)
						os.Exit(1)
					}
					return
				}

				// No version specified; find latest
				err = installProject(project, versionFilename, pack, &index)
				if err != nil {
					fmt.Printf("添加项目失败：%s\n", err)
					os.Exit(1)
				}
				return
			}
		}

		// Arguments weren't a valid slug/project ID, try to search for it instead (if it was not parsed as a URL)
		if projectID == "" || parsedSlug {
			err = installViaSearch(strings.Join(args, " "), versionFilename, !parsedSlug, pack, &index)
			if err != nil {
				fmt.Printf("添加项目失败：%s\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Printf("添加项目失败：%s\n", err)
			os.Exit(1)
		}
	},
}

func installVersionById(versionId string, versionFilename string, pack core.Pack, index *core.Index) error {
	version, err := mrDefaultClient.Versions.Get(versionId)
	if err != nil {
		return fmt.Errorf("获取版本 %s 失败：%v", versionId, err)
	}

	project, err := mrDefaultClient.Projects.Get(*version.ProjectID)
	if err != nil {
		return fmt.Errorf("获取项目 %s 失败：%v", *version.ProjectID, err)
	}

	return installVersion(project, version, versionFilename, pack, index)
}

func installViaSearch(query string, versionFilename string, autoAcceptFirst bool, pack core.Pack, index *core.Index) error {
	mcVersions, err := pack.GetSupportedMCVersions()
	if err != nil {
		return err
	}

	fmt.Println("正在搜索Modrinth...")

	results, err := getProjectIdsViaSearch(query, mcVersions)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		return errors.New("未找到任何项目")
	}

	if viper.GetBool("non-interactive") || (len(results) == 1 && autoAcceptFirst) {
		// Install the first project found
		project, err := mrDefaultClient.Projects.Get(*results[0].ProjectID)
		if err != nil {
			return err
		}

		return installProject(project, versionFilename, pack, index)
	}

	// Create menu for the user to choose the correct project
	menu := wmenu.NewMenu("选择一个数字：")
	menu.Option("取消", nil, false, nil)
	for i, v := range results {
		// Should be non-nil (Title is a required field)
		menu.Option(*v.Title, v, i == 0, nil)
	}

	menu.Action(func(menuRes []wmenu.Opt) error {
		if len(menuRes) != 1 || menuRes[0].Value == nil {
			return errors.New("已取消项目选择")
		}
		}

		// Get the selected project
		selectedProject, ok := menuRes[0].Value.(*modrinthApi.SearchResult)
		if !ok {
			return errors.New("从wmenu转换接口时出错")
		}

		// Install the selected project
		project, err := mrDefaultClient.Projects.Get(*selectedProject.ProjectID)
		if err != nil {
			return err
		}

		return installProject(project, versionFilename, pack, index)
	})

	return menu.Run()
}

func installProject(project *modrinthApi.Project, versionFilename string, pack core.Pack, index *core.Index) error {
	latestVersion, err := getLatestVersion(*project.ID, *project.Title, pack)
	if err != nil {
		return fmt.Errorf("获取最新版本失败：%v", err)
	}
	if latestVersion.ID == nil {
		return errors.New("mod不适用于配置的Minecraft版本（使用'packwiz settings acceptable-versions'命令以接受更多版本）或加载器")
	}

	return installVersion(project, latestVersion, versionFilename, pack, index)
}

const maxCycles = 20

type depMetadataStore struct {
	projectInfo *modrinthApi.Project
	versionInfo *modrinthApi.Version
	fileInfo    *modrinthApi.File
}

func installVersion(project *modrinthApi.Project, version *modrinthApi.Version, versionFilename string, pack core.Pack, index *core.Index) error {
	if len(version.Files) == 0 {
		return errors.New("版本没有任何附加文件")
	}

	if len(version.Dependencies) > 0 {
		// TODO: could get installed version IDs, and compare to install the newest - i.e. preferring pinned versions over getting absolute latest?
		installedProjects := getInstalledProjectIDs(index)
		isQuilt := slices.Contains(pack.GetCompatibleLoaders(), "quilt")
		mcVersion, err := pack.GetMCVersion()
		if err != nil {
			return err
		}

		var depMetadata []depMetadataStore
		var depProjectIDPendingQueue []string
		var depVersionIDPendingQueue []string

		for _, dep := range version.Dependencies {
			// TODO: recommend optional dependencies?
			if dep.DependencyType != nil && *dep.DependencyType == "required" {
				if dep.VersionID != nil {
					depVersionIDPendingQueue = append(depVersionIDPendingQueue, *dep.VersionID)
				} else {
					if dep.ProjectID != nil {
						depProjectIDPendingQueue = append(depProjectIDPendingQueue, mapDepOverride(*dep.ProjectID, isQuilt, mcVersion))
					}
				}
			}
		}

		if len(depProjectIDPendingQueue)+len(depVersionIDPendingQueue) > 0 {
			fmt.Println("正在查找依赖项...")

			cycles := 0
			for len(depProjectIDPendingQueue)+len(depVersionIDPendingQueue) > 0 && cycles < maxCycles {
				// Look up version IDs
				if len(depVersionIDPendingQueue) > 0 {
					depVersions, err := mrDefaultClient.Versions.GetMultiple(depVersionIDPendingQueue)
					if err == nil {
						for _, v := range depVersions {
							// Add project ID to queue
							depProjectIDPendingQueue = append(depProjectIDPendingQueue, mapDepOverride(*v.ProjectID, isQuilt, mcVersion))
						}
					} else {
						fmt.Printf("检索依赖数据时出错：%s\n", err.Error())
					}
					depVersionIDPendingQueue = depVersionIDPendingQueue[:0]
				}

				// Remove installed project IDs from dep queue
				i := 0
				for _, id := range depProjectIDPendingQueue {
					contains := slices.Contains(installedProjects, id)
					for _, dep := range depMetadata {
						if *dep.projectInfo.ID == id {
							contains = true
							break
						}
					}
					if !contains {
						depProjectIDPendingQueue[i] = id
						i++
					}
				}
				depProjectIDPendingQueue = depProjectIDPendingQueue[:i]

				// Clean up duplicates from dep queue (from deps on both QFAPI + FAPI)
				slices.Sort(depProjectIDPendingQueue)
				depProjectIDPendingQueue = slices.Compact(depProjectIDPendingQueue)

				if len(depProjectIDPendingQueue) == 0 {
					break
				}
				depProjects, err := mrDefaultClient.Projects.GetMultiple(depProjectIDPendingQueue)
				if err != nil {
					fmt.Printf("检索依赖数据时出错：%s\n", err.Error())
				}
				depProjectIDPendingQueue = depProjectIDPendingQueue[:0]

				for _, project := range depProjects {
					if project.ID == nil {
						return errors.New("获取依赖数据失败：响应无效")
					}
					// Get latest version - could reuse version lookup data but it's not as easy (particularly since the version won't necessarily be the latest)
					latestVersion, err := getLatestVersion(*project.ID, *project.Title, pack)
					if err != nil {
						fmt.Printf("获取依赖 %v 的最新版本失败：%v\n", *project.Title, err)
						continue
					}

					for _, dep := range version.Dependencies {
						// TODO: recommend optional dependencies?
						if dep.DependencyType != nil && *dep.DependencyType == "required" {
							if dep.ProjectID != nil {
								depProjectIDPendingQueue = append(depProjectIDPendingQueue, mapDepOverride(*dep.ProjectID, isQuilt, mcVersion))
							}
							if dep.VersionID != nil {
								depVersionIDPendingQueue = append(depVersionIDPendingQueue, *dep.VersionID)
							}
						}
					}

					var file = latestVersion.Files[0]
					// Prefer the primary file
					for _, v := range latestVersion.Files {
						if *v.Primary {
							file = v
						}
					}

					depMetadata = append(depMetadata, depMetadataStore{
						projectInfo: project,
						versionInfo: latestVersion,
						fileInfo:    file,
					})
				}

				cycles++
			}
			if cycles >= maxCycles {
				return errors.New("依赖项递归太深，尝试增加maxCycles")
			}

			if len(depMetadata) > 0 {
				fmt.Println("找到依赖项：")
				for _, v := range depMetadata {
					fmt.Println(*v.projectInfo.Title)
				}

				if cmdshared.PromptYesNo("要添加它们吗？[Y/n]: ") {
					for _, v := range depMetadata {
						err := createFileMeta(v.projectInfo, v.versionInfo, v.fileInfo, pack, index)
						if err != nil {
							return err
						}
						fmt.Printf("依赖项 \"%s\" 成功添加！（%s）\n", *v.projectInfo.Title, *v.fileInfo.Filename)
					}
				}
			} else {
				fmt.Println("所有依赖项都已添加！")
			}
		}
	}

	var file = version.Files[0]
	// Prefer the primary file
	for _, v := range version.Files {
		if (*v.Primary) || (versionFilename != "" && versionFilename == *v.Filename) {
			file = v
		}
	}
	// TODO: handle optional/required resource pack files

	// Create the metadata file
	err := createFileMeta(project, version, file, pack, index)
	if err != nil {
		return err
	}

	err = index.Write()
	if err != nil {
		return err
	}
	err = pack.UpdateIndexHash()
	if err != nil {
		return err
	}
	err = pack.Write()
	if err != nil {
		return err
	}

	fmt.Printf("项目 \"%s\" 成功添加！（%s）\n", *project.Title, *file.Filename)
	return nil
}

func createFileMeta(project *modrinthApi.Project, version *modrinthApi.Version, file *modrinthApi.File, pack core.Pack, index *core.Index) error {
	updateMap := make(map[string]map[string]interface{})

	var err error
	updateMap["modrinth"], err = mrUpdateData{
		ProjectID:        *project.ID,
		InstalledVersion: *version.ID,
	}.ToMap()
	if err != nil {
		return err
	}

	side := getSide(project)
	if side == "" {
		fmt.Println("警告：项目没有支持的side；假设为通用。Server: " + *project.ServerSide + " Client: " + *project.ClientSide)
		side = core.UniversalSide
	}

	algorithm, hash := getBestHash(file)
	if algorithm == "" {
		return errors.New("文件没有哈希")
	}

	modMeta := core.Mod{
		Name:     *project.Title,
		FileName: *file.Filename,
		Side:     side,
		Download: core.ModDownload{
			URL:        *file.URL,
			HashFormat: algorithm,
			Hash:       hash,
		},
		Update: updateMap,
	}
	var path string
	folder := viper.GetString("meta-folder")
	if folder == "" {
		folder, err = getProjectTypeFolder(*project.ProjectType, version.Loaders, pack.GetCompatibleLoaders())
		if err != nil {
			return err
		}
	}
	if project.Slug != nil {
		path = modMeta.SetMetaPath(filepath.Join(viper.GetString("meta-folder-base"), folder, *project.Slug+core.MetaExtension))
	} else {
		path = modMeta.SetMetaPath(filepath.Join(viper.GetString("meta-folder-base"), folder, core.SlugifyName(*project.Title)+core.MetaExtension))
	}

	// If the file already exists, this will overwrite it!!!
	// TODO: Should this be improved?
	// Current strategy is to go ahead and do stuff without asking, with the assumption that you are using
	// VCS anyway.

	format, hash, err := modMeta.Write()
	if err != nil {
		return err
	}
	return index.RefreshFileWithHash(path, format, hash, true)
}

var projectIDFlag string
var versionIDFlag string
var versionFilenameFlag string

func init() {
	modrinthCmd.AddCommand(installCmd)

	installCmd.Flags().StringVar(&projectIDFlag, "project-id", "", "要使用的Modrinth项目ID")
	installCmd.Flags().StringVar(&versionIDFlag, "version-id", "", "要使用的Modrinth版本ID")
	installCmd.Flags().StringVar(&versionFilenameFlag, "version-filename", "", "要使用的Modrinth版本文件名")
}
