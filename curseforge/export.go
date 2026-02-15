package curseforge

import (
	"archive/zip"
	"bufio"
	"fmt"
	"os"
	"strconv"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/packwiz/packwiz/curseforge/packinterop"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "将当前模组包导出为.curseforge的.zip文件",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		side := viper.GetString("curseforge.export.side")
		if side != core.UniversalSide && side != core.ServerSide && side != core.ClientSide {
			fmt.Printf("无效的side %q，必须是client、server或both（默认）之一\n", side)
			os.Exit(1)
		}

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
		// Do a refresh to ensure files are up to date
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

		fmt.Println("正在读取外部文件...")
		mods, err := index.LoadAllMods()
		if err != nil {
			fmt.Printf("读取文件时出错：%v\n", err)
			os.Exit(1)
		}
		i := 0
		// Filter mods by side
		// TODO: opt-in optional disabled filtering?
		for _, mod := range mods {
			if mod.Side == side || mod.Side == core.EmptySide || mod.Side == core.UniversalSide || side == core.UniversalSide {
				mods[i] = mod
				i++
			}
		}
		mods = mods[:i]

		var exportData cfExportData
		exportDataUnparsed, ok := pack.Export["curseforge"]
		if ok {
			exportData, err = parseExportData(exportDataUnparsed)
			if err != nil {
				fmt.Printf("解析导出元数据失败：%s\n", err.Error())
				os.Exit(1)
			}
		}

		fileName := viper.GetString("curseforge.export.output")
		if fileName == "" {
			fileName = pack.GetPackName() + ".zip"
		}

		expFile, err := os.Create(fileName)
		if err != nil {
			fmt.Printf("创建zip失败：%s\n", err.Error())
			os.Exit(1)
		}
		exp := zip.NewWriter(expFile)

		// Add an overrides folder even if there are no files to go in it
		_, err = exp.Create("overrides/")
		if err != nil {
			fmt.Printf("添加overrides文件夹失败：%s\n", err.Error())
			os.Exit(1)
		}

		cfFileRefs := make([]packinterop.AddonFileReference, 0, len(mods))
		nonCfMods := make([]*core.Mod, 0)
		for _, mod := range mods {
			projectRaw, ok := mod.GetParsedUpdateData("curseforge")
			// If the mod has curseforge metadata, add it to cfFileRefs
			if ok {
				p := projectRaw.(cfUpdateData)
				cfFileRefs = append(cfFileRefs, packinterop.AddonFileReference{
					ProjectID:        p.ProjectID,
					FileID:           p.FileID,
					OptionalDisabled: mod.Option != nil && mod.Option.Optional && !mod.Option.Default,
				})
			} else {
				nonCfMods = append(nonCfMods, mod)
			}
		}

		// Download external files and save directly into the zip
		if len(nonCfMods) > 0 {
			fmt.Printf("正在检索 %v 个外部文件以存储在模组包zip中...\n", len(nonCfMods))
			cmdshared.PrintDisclaimer(true)

			session, err := core.CreateDownloadSession(nonCfMods, []string{})
			if err != nil {
				fmt.Printf("检索外部文件时出错：%v\n", err)
				os.Exit(1)
			}

			cmdshared.ListManualDownloads(session)

			for dl := range session.StartDownloads() {
				_ = cmdshared.AddToZip(dl, exp, "overrides", &index)
			}

			err = session.SaveIndex()
			if err != nil {
				fmt.Printf("保存缓存索引时出错：%v\n", err)
				os.Exit(1)
			}
		}

		manifestFile, err := exp.Create("manifest.json")
		if err != nil {
			_ = exp.Close()
			_ = expFile.Close()
			fmt.Println("创建清单时出错：" + err.Error())
			os.Exit(1)
		}

		err = packinterop.WriteManifestFromPack(pack, cfFileRefs, exportData.ProjectID, manifestFile)
		if err != nil {
			_ = exp.Close()
			_ = expFile.Close()
			fmt.Println("写入清单时出错：" + err.Error())
			os.Exit(1)
		}

		err = createModlist(exp, mods)
		if err != nil {
			_ = exp.Close()
			_ = expFile.Close()
			fmt.Println("创建模组列表时出错：" + err.Error())
			os.Exit(1)
		}

		cmdshared.AddNonMetafileOverrides(&index, exp)

		err = exp.Close()
		if err != nil {
			fmt.Println("写入导出文件时出错：" + err.Error())
			os.Exit(1)
		}
		err = expFile.Close()
		if err != nil {
			fmt.Println("写入导出文件时出错：" + err.Error())
			os.Exit(1)
		}

		fmt.Println("模组包已导出到 " + fileName)
	},
}

func createModlist(zw *zip.Writer, mods []*core.Mod) error {
	modlistFile, err := zw.Create("modlist.html")
	if err != nil {
		return err
	}

	w := bufio.NewWriter(modlistFile)

	_, err = w.WriteString("<ul>\r\n")
	if err != nil {
		return err
	}
	for _, mod := range mods {
		projectRaw, ok := mod.GetParsedUpdateData("curseforge")
		if !ok {
			// TODO: read homepage URL or something similar?
			// TODO: how to handle mods that don't have metadata???
			_, err = w.WriteString("<li>" + mod.Name + "</li>\r\n")
			if err != nil {
				return err
			}
			continue
		}
		project := projectRaw.(cfUpdateData)
		_, err = w.WriteString("<li><a href=\"https://www.curseforge.com/projects/" + strconv.FormatUint(uint64(project.ProjectID), 10) + "\">" + mod.Name + "</a></li>\r\n")
		if err != nil {
			return err
		}
	}
	_, err = w.WriteString("</ul>\r\n")
	if err != nil {
		return err
	}
	return w.Flush()
}

func init() {
	curseforgeCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringP("side", "s", "client", "导出模组所使用的端")
	_ = viper.BindPFlag("curseforge.export.side", exportCmd.Flags().Lookup("side"))
	exportCmd.Flags().StringP("output", "o", "", "将模组包导出到哪个文件")
	_ = viper.BindPFlag("curseforge.export.output", exportCmd.Flags().Lookup("output"))
}
