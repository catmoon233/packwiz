package cmdshared

import (
	"archive/zip"
	"fmt"
	"github.com/packwiz/packwiz/core"
	"io"
	"os"
	"path"
	"path/filepath"
)

func ListManualDownloads(session core.DownloadSession) {
	manualDownloads := session.GetManualDownloads()
	if len(manualDownloads) > 0 {
		fmt.Printf("发现 %v 个手动下载；这些模组无法由 packwiz 下载（由于 API 限制），必须手动下载：\n",
			len(manualDownloads))
		for _, dl := range manualDownloads {
			fmt.Printf("%s (%s) from %s\n", dl.Name, dl.FileName, dl.URL)
		}
		cacheDir, err := core.GetPackwizCache()
		if err != nil {
			fmt.Printf("Error locating cache folder: %v", err)
			os.Exit(1)
		}

		fmt.Printf("完成后，将这些文件放在 %s 中并重新运行此命令。\n",
			filepath.Join(cacheDir, core.DownloadCacheImportFolder))
		os.Exit(1)
	}
}

func AddToZip(dl core.CompletedDownload, exp *zip.Writer, dir string, index *core.Index) bool {
	if dl.Error != nil {
		fmt.Printf("下载 %s (%s) 失败：%v\n", dl.Mod.Name, dl.Mod.FileName, dl.Error)
		return false
	}
	for _, warning := range dl.Warnings {
		fmt.Printf("%s (%s) 的警告：%v\n", dl.Mod.Name, dl.Mod.FileName, warning)
	}

	p, err := index.RelIndexPath(dl.Mod.GetDestFilePath())
	if err != nil {
		fmt.Printf("解析外部文件时出错：%v\n", err)
		return false
	}
	modFile, err := exp.Create(path.Join(dir, p))
	if err != nil {
		fmt.Printf("创建元数据文件 %s 时出错：%v\n", p, err)
		return false
	}
	_, err = io.Copy(modFile, dl.File)
	if err != nil {
		fmt.Printf("复制文件 %s 时出错：%v\n", p, err)
		return false
	}
	err = dl.File.Close()
	if err != nil {
		fmt.Printf("关闭文件 %s 时出错：%v\n", p, err)
		return false
	}

	fmt.Printf("%s (%s) 已添加到 zip\n", dl.Mod.Name, dl.Mod.FileName)
	return true
}

// AddNonMetafileOverrides saves all non-metadata files into an overrides folder in the zip
func AddNonMetafileOverrides(index *core.Index, exp *zip.Writer) {
	for p, v := range index.Files {
		if !v.IsMetaFile() {
			file, err := exp.Create(path.Join("overrides", p))
			if err != nil {
				fmt.Printf("创建文件时出错：%s\n", err.Error())
				// TODO: exit(1)?
				continue
			}
			// Attempt to read the file from disk, without checking hashes (assumed to have no errors)
			src, err := os.Open(index.ResolveIndexPath(p))
			if err != nil {
				_ = src.Close()
				fmt.Printf("读取文件时出错：%s\n", err.Error())
				// TODO: exit(1)?
				continue
			}
			_, err = io.Copy(file, src)
			if err != nil {
				_ = src.Close()
				fmt.Printf("复制文件时出错：%s\n", err.Error())
				// TODO: exit(1)?
				continue
			}

			_ = src.Close()
		}
	}
}

func PrintDisclaimer(isCf bool) {
	fmt.Println("免责声明：您有责任确保您遵守所有许可证或获得适当的许可，用于以下\"添加到 zip\"的文件")
	if isCf {
		fmt.Println("注意，CurseForge 模组包中捆绑的模组必须在批准的非 CurseForge 模组列表中")
		fmt.Println("packwiz 目前无法在模组站点之间匹配元数据 - 如果其中任何一个可以从 CurseForge 获得，您应该更改它们以使用 CurseForge 元数据（例如，通过使用 cf 命令重新添加它们）")
	} else {
		fmt.Println("packwiz 目前无法在模组站点之间匹配元数据 - 如果其中任何一个可以从 Modrinth 获得，您应该更改它们以使用 Modrinth 元数据（例如，通过使用 mr 命令重新添加它们）")
	}
	fmt.Println()
}
