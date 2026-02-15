package cmd

import (
	"fmt"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/pflag"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var packFile string
var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "packwiz",
	Short: "创建 Minecraft 模组包的命令行工具",
}

// Execute starts the root command for packwiz
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// Add adds a new command as a subcommand to packwiz
func Add(newCommand *cobra.Command) {
	rootCmd.AddCommand(newCommand)
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&packFile, "pack-file", "pack.toml", "要使用的模组包元数据文件")
	_ = viper.BindPFlag("pack-file", rootCmd.PersistentFlags().Lookup("pack-file"))

	// Make mods-folder an alias for meta-folder
	viper.RegisterAlias("mods-folder", "meta-folder")
	rootCmd.SetGlobalNormalizationFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "mods-folder" {
			return "meta-folder"
		}
		return pflag.NormalizedName(name)
	})

	var metaFolder string
	rootCmd.PersistentFlags().StringVar(&metaFolder, "meta-folder", "", "添加新元数据文件的文件夹，默认为基于类别的文件夹（mods、resourcepacks 等；如果类别未知则使用当前目录）")
	_ = viper.BindPFlag("meta-folder", rootCmd.PersistentFlags().Lookup("meta-folder"))

	var metaFolderBase string
	rootCmd.PersistentFlags().StringVar(&metaFolderBase, "meta-folder-base", ".", "解析 meta-folder 的基础文件夹，默认为当前目录（因此您可以将所有模组等放在子文件夹中，同时仍使用默认行为）")
	_ = viper.BindPFlag("meta-folder-base", rootCmd.PersistentFlags().Lookup("meta-folder-base"))

	defaultCacheDir, err := core.GetPackwizCache()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	rootCmd.PersistentFlags().String("cache", defaultCacheDir, "packwiz 缓存已下载模组的目录")
	_ = viper.BindPFlag("cache.directory", rootCmd.PersistentFlags().Lookup("cache"))

	file, err := core.GetPackwizLocalStore()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	file = filepath.Join(file, ".packwiz.toml")
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "要使用的配置文件（默认 \""+file+"\"）")

	var nonInteractive bool
	rootCmd.PersistentFlags().BoolVarP(&nonInteractive, "yes", "y", false, "接受所有提示的默认或\"是\"选项（非交互模式）- 可能在搜索结果中选择不需要的选项")
	_ = viper.BindPFlag("non-interactive", rootCmd.PersistentFlags().Lookup("yes"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		dir, err := core.GetPackwizLocalStore()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		viper.AddConfigPath(dir)
		viper.SetConfigName(".packwiz")
	}

	// Read in environment variables that match
	viper.SetEnvPrefix("packwiz")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("正在使用配置文件：", viper.ConfigFileUsed())
	}
}
