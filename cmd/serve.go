package cmd

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var refreshMutex sync.RWMutex

//go:embed serve-templates/index.html
var indexPage string

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:     "serve",
	Short:   "运行本地开发服务器",
	Long:    `运行本地 HTTP 服务器用于开发，在查询索引时自动刷新`,
	Aliases: []string{"server"},
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		port := strconv.Itoa(viper.GetInt("serve.port"))

		if viper.GetBool("serve.basic") {
			http.Handle("/", http.FileServer(http.Dir(".")))
		} else {
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
			packServeDir := filepath.Dir(viper.GetString("pack-file"))
			packFileName := filepath.Base(viper.GetString("pack-file"))

			t, err := template.New("index-page").Parse(indexPage)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			indexPageBuf := new(bytes.Buffer)
			err = t.Execute(indexPageBuf, struct{ Port string }{Port: port})
			if err != nil {
				panic(fmt.Errorf("failed to compile index page template: %w", err))
			}

			// Force-disable no-internal-hashes mode (equiv to --build flag in refresh) for serving over HTTP
			if viper.GetBool("no-internal-hashes") {
				fmt.Println("注意：已设置 no-internal-hashes 模式；仍为 packwiz-installer 写入哈希值 - 运行 packwiz refresh 以删除它们。")
				viper.Set("no-internal-hashes", false)
			}

			http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/" {
					_, _ = w.Write(indexPageBuf.Bytes())
					return
				}

				// Relative to pack.toml
				urlPath := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(req.URL.Path, "/")), "/")
				// Convert to absolute
				destPath := filepath.Join(packServeDir, filepath.FromSlash(urlPath))
				// Relativisation needs to be done using filepath, as path doesn't have Rel!
				// (now using index util function)
				// Relative to index.toml ("pack root")
				indexRelPath, err := index.RelIndexPath(destPath)
				if err != nil {
					fmt.Println("解析路径失败", err)
					return
				}

				if urlPath == path.Clean(pack.Index.File) {
					// Must be done here, to ensure all paths gain the lock at some point
					refreshMutex.RLock()
				} else if urlPath == packFileName { // Only need to compare name - already relative to pack.toml
					if viper.GetBool("serve.refresh") {
						// Get write lock, to do a refresh
						refreshMutex.Lock()
						// Reload pack and index (might have changed on disk)
						err = doServeRefresh(&pack, &index)
						if err != nil {
							fmt.Println("刷新模组包失败", err)
							return
						}

						// Downgrade to a read lock
						refreshMutex.Unlock()
					}
					refreshMutex.RLock()
				} else {
					refreshMutex.RLock()
					// Only allow indexed files
					if _, found := index.Files[indexRelPath]; !found {
						fmt.Printf("文件未找到：%s\n", destPath)
						refreshMutex.RUnlock()
						w.WriteHeader(404)
						_, _ = w.Write([]byte("File not found"))
						return
					}
				}
				defer refreshMutex.RUnlock()

				f, err := os.Open(destPath)
				if err != nil {
					fmt.Printf("读取文件 \"%s\" 时出错：%s\n", destPath, err)
					w.WriteHeader(404)
					_, _ = w.Write([]byte("File not found"))
					return
				}
				_, err = io.Copy(w, f)
				err2 := f.Close()
				if err == nil {
					err = err2
				}
				if err != nil {
					fmt.Printf("读取文件 \"%s\" 时出错：%s\n", destPath, err)
					w.WriteHeader(500)
					_, _ = w.Write([]byte("Failed to read file"))
					return
				}
			})
		}

		fmt.Println("运行在端口 " + port)
		err := http.ListenAndServe(":"+port, nil)
		if err != nil {
			fmt.Printf("运行服务器时出错：%s\n", err)
			os.Exit(1)
		}
	},
}

func doServeRefresh(pack *core.Pack, index *core.Index) error {
	var err error
	*pack, err = core.LoadPack()
	if err != nil {
		return err
	}
	*index, err = pack.LoadIndex()
	if err != nil {
		return err
	}
	err = index.Refresh()
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
	fmt.Println("索引已刷新！")

	return nil
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().IntP("port", "p", 8080, "运行服务器的端口")
	_ = viper.BindPFlag("serve.port", serveCmd.Flags().Lookup("port"))
	serveCmd.Flags().BoolP("refresh", "r", true, "自动刷新索引文件")
	_ = viper.BindPFlag("serve.refresh", serveCmd.Flags().Lookup("refresh"))
	serveCmd.Flags().Bool("basic", false, "禁用刷新并允许目录中的所有文件，而不仅仅是索引中列出的文件")
	_ = viper.BindPFlag("serve.basic", serveCmd.Flags().Lookup("basic"))
}
