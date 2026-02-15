package curseforge

import (
	"fmt"
	"github.com/aviddiviner/go-murmur"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
)

// TODO: make all of this less bad and hardcoded

// detectCmd represents the detect command
var detectCmd = &cobra.Command{
	Use:   "detect",
	Short: "检测mods文件夹中的.jar文件（实验性）",
	Args:  cobra.NoArgs,
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

		// Walk files in the mods folder
		var hashes []uint32
		modPaths := make(map[uint32]string)
		err = filepath.Walk("mods", func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".jar") && !strings.HasSuffix(path, ".litemod") {
				// TODO: make this less bad
				return nil
			}
			fmt.Println("正在计算哈希 " + path)
			bytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash := getByteArrayHash(bytes)
			hashes = append(hashes, hash)
			modPaths[hash] = path
			return nil
		})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("找到 %d 个文件，正在提交...\n", len(hashes))

		res, err := cfDefaultClient.getFingerprintInfo(hashes)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Printf("成功匹配 %d 个文件\n", len(res.ExactFingerprints))
		if len(res.PartialMatches) > 0 {
			fmt.Println("以下指纹是部分匹配的，我不知道该怎么办！！！")
			for _, v := range res.PartialMatches {
				fmt.Printf("%s (%d)", modPaths[v], v)
			}
		}
		if len(res.UnmatchedFingerprints) > 0 {
			fmt.Printf("无法匹配以下 %d 个文件：\n", len(res.UnmatchedFingerprints))
			for _, v := range res.UnmatchedFingerprints {
				fmt.Printf("%s (%d)\n", modPaths[v], v)
			}
		}

		fmt.Println("正在检索元数据...")
		ids := make([]uint32, len(res.ExactMatches))
		for i, v := range res.ExactMatches {
			ids[i] = v.ID
		}
		modInfos, err := cfDefaultClient.getModInfoMultiple(ids)
		if err != nil {
			fmt.Printf("检索元数据失败：%v", err)
			os.Exit(1)
		}
		modInfosMap := make(map[uint32]modInfo)
		for _, v := range modInfos {
			modInfosMap[v.ID] = v
		}

		fmt.Println("正在创建元数据文件...")
		for _, v := range res.ExactMatches {
			err = createModFile(modInfosMap[v.ID], v.File, &index, false)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			path, ok := modPaths[v.File.Fingerprint]
			if ok {
				err = os.Remove(path)
				if err != nil {
					fmt.Println(err)
					os.Exit(1)
				}
			}
		}
		fmt.Println("检测完成！")

		err = index.Refresh()
		if err != nil {
			fmt.Println(err)
			return
		}
		err = index.Write()
		if err != nil {
			fmt.Println(err)
			return
		}
		err = pack.UpdateIndexHash()
		if err != nil {
			fmt.Println(err)
			return
		}
		err = pack.Write()
		if err != nil {
			fmt.Println(err)
			return
		}
	},
}

func init() {
	curseforgeCmd.AddCommand(detectCmd)
}

func getByteArrayHash(bytes []byte) uint32 {
	return murmur.MurmurHash2(computeNormalizedArray(bytes), 1)
}

func computeNormalizedArray(bytes []byte) []byte {
	var newArray []byte
	for _, b := range bytes {
		if !isWhitespaceCharacter(b) {
			newArray = append(newArray, b)
		}
	}
	return newArray
}

func isWhitespaceCharacter(b byte) bool {
	return b == 9 || b == 10 || b == 13 || b == 32
}
