package modrinth

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"

	modrinthApi "codeberg.org/jmansfield/go-modrinth/modrinth"
	"github.com/packwiz/packwiz/cmd"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/unascribed/FlexVer/go/flexver"
)

var modrinthCmd = &cobra.Command{
	Use:     "modrinth",
	Aliases: []string{"mr"},
	Short:   "管理基于Modrinth的模组",
}

var mrDefaultClient = modrinthApi.NewClient(&http.Client{})

func init() {
	cmd.Add(modrinthCmd)
	core.Updaters["modrinth"] = mrUpdater{}

	mrDefaultClient.UserAgent = core.UserAgent
}

func getProjectIdsViaSearch(query string, versions []string) ([]*modrinthApi.SearchResult, error) {
	facets := make([]string, 0)
	for _, v := range versions {
		facets = append(facets, "versions:"+v)
	}

	res, err := mrDefaultClient.Projects.Search(&modrinthApi.SearchOptions{
		Limit:  5,
		Index:  "relevance",
		Facets: [][]string{facets},
		Query:  query,
	})

	if err != nil {
		return nil, err
	}
	return res.Hits, nil
}

// 无论配置的mod加载器如何都支持的"加载器"
var defaultMRLoaders = []string{
	// TODO: 检查是否安装了Canvas/Iris/Optifine？建议安装它们？
	"canvas",
	"iris",
	"optifine",
	"vanilla",   // Core shaders
	"minecraft", // Resource packs
}

var withDatapackPathMRLoaders = []string{
	"canvas",
	"iris",
	"optifine",
	"vanilla",   // Core shaders
	"minecraft", // Resource packs
	// TODO: 检查是否安装了datapack加载器；建议安装一个？
	"datapack", // Datapacks (requires a datapack loader)
}

var loaderFolders = map[string]string{
	"quilt":      "mods",
	"fabric":     "mods",
	"forge":      "mods",
	"neoforge":   "mods",
	"liteloader": "mods",
	"modloader":  "mods",
	"rift":       "mods",
	"bukkit":     "plugins",
	"spigot":     "plugins",
	"paper":      "plugins",
	"purpur":     "plugins",
	"sponge":     "plugins",
	"bungeecord": "plugins",
	"waterfall":  "plugins",
	"velocity":   "plugins",
	"canvas":     "resourcepacks",
	"iris":       "shaderpacks",
	"optifine":   "shaderpacks",
	"vanilla":    "resourcepacks",
}

// 用于比较具有相同版本的文件的加载器类型的首选项列表 - 更偏向的索引更低
var loaderPreferenceList = []string{
	// 偏好quilt版本而不是fabric版本
	"quilt",
	"fabric",
	// 偏好neoforge版本而不是forge版本
	"neoforge",
	"forge",
	"liteloader",
	"modloader",
	"rift",
	// 偏好mod而不是插件
	"sponge",
	// 偏好更新的Bukkit分支
	"purpur",
	"paper",
	"spigot",
	"bukkit",
	"velocity",
	// 偏好更新的BungeeCord分支
	"waterfall",
	"bungeecord",
	// 偏好Canvas着色器而不是Iris着色器而不是Optifine着色器而不是核心着色器
	"canvas",
	"iris",
	"optifine",
	"vanilla",
	// 偏好mod而不是datapacks
	"datapack",
	// 偏好mod而不是资源包？！这只是为了完整性而存在
	"minecraft",
}

// 应该被视为与键相同的加载器组，如果两个版本都支持该键
// 即键是更"通用"的加载器；对它的支持意味着对整个组的支持
// 例如 [quilt, fabric] 应该与 [fabric] 比较相等（但小于 [quilt]，因为Quilt支持并不意味着Fabric支持）
// 当作者忘记将Quilt/Purpur等添加到所有版本时，这很有用
// TODO: 从后端源抽象
var loaderCompatGroups = map[string][]string{
	"fabric":     {"quilt"},
	"forge":      {"neoforge"},
	"bukkit":     {"purpur", "paper", "spigot"},
	"bungeecord": {"waterfall"},
}

func getProjectTypeFolder(projectType string, fileLoaders []string, packLoaders []string) (string, error) {
	if projectType == "modpack" {
		return "", errors.New("此命令不应用于添加Modrinth模组包，并且尚不支持导入Modrinth模组包")
	} else if projectType == "resourcepack" {
		return "resourcepacks", nil
	} else if projectType == "shader" {
		bestLoaderIdx := math.MaxInt
		for _, v := range fileLoaders {
			idx := slices.Index(loaderPreferenceList, v)
			if idx != -1 && idx < bestLoaderIdx {
				bestLoaderIdx = idx
			}
		}
		if bestLoaderIdx > -1 && bestLoaderIdx < math.MaxInt {
			return loaderFolders[loaderPreferenceList[bestLoaderIdx]], nil
		}
		return "shaderpacks", nil
	} else if projectType == "mod" {
		// 在加载器列表中查找包加载器（注意这当前过滤为quilt/fabric/neoforge/forge）
		bestLoaderIdx := math.MaxInt
		for _, v := range fileLoaders {
			if slices.Contains(packLoaders, v) {
				idx := slices.Index(loaderPreferenceList, v)
				if idx != -1 && idx < bestLoaderIdx {
					bestLoaderIdx = idx
				}
			}
		}
		if bestLoaderIdx > -1 && bestLoaderIdx < math.MaxInt {
			return loaderFolders[loaderPreferenceList[bestLoaderIdx]], nil
		}

		// Datapack加载器是"datapack"
		if slices.Contains(fileLoaders, "datapack") {
			if viper.GetString("datapack-folder") != "" {
				return viper.GetString("datapack-folder"), nil
			} else {
				return "", errors.New("设置datapack-folder选项以使用datapacks")
			}
		}
		// 默认mod类型为"mods"
		return "mods", nil
	} else {
		return "", fmt.Errorf("未知的项目类型 %s", projectType)
	}
}

var urlRegexes = [...]*regexp.Regexp{
	// Slug/version number regex from https://github.com/modrinth/labrinth/blob/1679a3f844497d756d0cf272c5374a5236eabd42/src/util/validate.rs#L8
	regexp.MustCompile("^https?://(www.)?modrinth\\.com/(?P<urlCategory>[^/]+)/(?P<slug>[a-zA-Z0-9!@$()`.+,_\"-]{3,64})(?:/version/(?P<version>[a-zA-Z0-9!@$()`.+,_\"-]{1,32}))?"),
	// Version/project IDs are more restrictive: [a-zA-Z0-9]+ (base62)
	regexp.MustCompile("^https?://cdn\\.modrinth\\.com/data/(?P<slug>[a-zA-Z0-9]+)/versions/(?P<versionID>[a-zA-Z0-9]+)/(?P<filename>[^/]+)$"),
	regexp.MustCompile("^(?P<slug>[a-zA-Z0-9!@$()`.+,_\"-]{3,64})$"),
}

const slugRegexIdx = 2

var urlCategories = []string{
	"mod", "plugin", "datapack", "shader", "resourcepack", "modpack",
}

func parseSlugOrUrl(input string, slug *string, version *string, versionID *string, filename *string) (parsedSlug bool, err error) {
	for regexIdx, r := range urlRegexes {
		matches := r.FindStringSubmatch(input)
		if matches != nil {
			if i := r.SubexpIndex("urlCategory"); i >= 0 {
				if !slices.Contains(urlCategories, matches[i]) {
					err = errors.New("unknown project type: " + matches[i])
					return
				}
			}
			if i := r.SubexpIndex("slug"); i >= 0 {
				*slug = matches[i]
			}
			if i := r.SubexpIndex("version"); i >= 0 {
				*version = matches[i]
			}
			if i := r.SubexpIndex("versionID"); i >= 0 {
				*versionID = matches[i]
			}
			if i := r.SubexpIndex("filename"); i >= 0 {
				var parsed string
				parsed, err = url.PathUnescape(matches[i])
				if err != nil {
					return
				}
				*filename = parsed
			}
			parsedSlug = regexIdx == slugRegexIdx
			return
		}
	}
	return
}

func compareLoaderLists(a []string, b []string) int32 {
	var compat []string
	for k, v := range loaderCompatGroups {
		if slices.Contains(a, k) && slices.Contains(b, k) {
			// Prerequisite loader is in both lists; add compat group
			compat = append(compat, v...)
		}
	}
		// 偏好加载器；主要是Quilt而不是Fabric，mod而不是datapacks（Modrinth后端处理过滤）
	minIdxA := math.MaxInt
	for _, v := range a {
		if slices.Contains(compat, v) {
			// 比较时忽略compat组中的加载器
			continue
		}
		idx := slices.Index(loaderPreferenceList, v)
		if idx != -1 && idx < minIdxA {
			minIdxA = idx
		}
	}
	minIdxB := math.MaxInt
	for _, v := range b {
		if slices.Contains(compat, v) {
			// 比较时忽略compat组中的加载器
			continue
		}
		idx := slices.Index(loaderPreferenceList, v)
	if idx != -1 && idx < minIdxA {
		return 1 // B有更偏好的加载器
		}
		if idx != -1 && idx < minIdxB {
			minIdxB = idx
		}
	}
	if minIdxA < minIdxB {
		return -1 // A有更偏好的加载器
	}
	return 0
}

func findLatestVersion(versions []*modrinthApi.Version, gameVersions []string, useFlexVer bool) *modrinthApi.Version {
	latestValidVersion := versions[0]
	bestGameVersion := core.HighestSliceIndex(gameVersions, versions[0].GameVersions)
	for _, v := range versions[1:] {
		gameVersionIdx := core.HighestSliceIndex(gameVersions, v.GameVersions)

		var compare int32
		if useFlexVer {
			// Use FlexVer to compare versions
			compare = flexver.Compare(*v.VersionNumber, *latestValidVersion.VersionNumber)
		}

		if compare == 0 {
			// Prefer later specified game versions (main version specified last)
			compare = int32(gameVersionIdx - bestGameVersion)
		}
		if compare == 0 {
			compare = compareLoaderLists(latestValidVersion.Loaders, v.Loaders)
		}
		if compare == 0 {
			// 其他比较相等，按日期比较
			if v.DatePublished.After(*latestValidVersion.DatePublished) {
				compare = 1
			}
		}
		if compare > 0 {
			latestValidVersion = v
			bestGameVersion = gameVersionIdx
		}
	}

	return latestValidVersion
}

func getLatestVersion(projectID string, name string, pack core.Pack) (*modrinthApi.Version, error) {
	gameVersions, err := pack.GetSupportedMCVersions()
	if err != nil {
		return nil, err
	}
	var loaders []string
	if viper.GetString("datapack-folder") != "" {
		loaders = append(pack.GetCompatibleLoaders(), withDatapackPathMRLoaders...)
	} else {
		loaders = append(pack.GetCompatibleLoaders(), defaultMRLoaders...)
	}

	result, err := mrDefaultClient.Versions.ListVersions(projectID, modrinthApi.ListVersionsOptions{
		GameVersions: gameVersions,
		Loaders:      loaders,
	})
	if err != nil {
		return nil, fmt.Errorf("获取最新版本失败：%w", err)
	}
	if len(result) == 0 {
		// TODO: 重试时指定datapack，以确定问题是什么？或者只是请求所有并稍后过滤
		return nil, errors.New("未找到有效版本\n\t使用'packwiz settings acceptable-versions'命令以接受更多游戏版本\n\t要使用datapacks，请添加一个datapack加载器mod，并使用该mod加载datapacks的文件夹指定datapack-folder选项")
	}

	// TODO: 始终使用flexver进行比较的选项？
	// TODO: 询问用户使用哪一个？
	flexverLatest := findLatestVersion(result, gameVersions, true)
	releaseDateLatest := findLatestVersion(result, gameVersions, false)
	if flexverLatest != releaseDateLatest && releaseDateLatest.VersionNumber != nil && flexverLatest.VersionNumber != nil {
		fmt.Printf("警告：%s的Modrinth版本在最新版本号和最新发布日期之间不一致（%s vs %s）\n", name, *flexverLatest.VersionNumber, *releaseDateLatest.VersionNumber)
	}

	return releaseDateLatest, nil
}

func getSide(mod *modrinthApi.Project) string {
	server := shouldDownloadOnSide(*mod.ServerSide)
	client := shouldDownloadOnSide(*mod.ClientSide)

	if server && client {
		return core.UniversalSide
	} else if server {
		return core.ServerSide
	} else if client {
		return core.ClientSide
	} else {
		return ""
	}
}

func shouldDownloadOnSide(side string) bool {
	return side == "required" || side == "optional"
}

func getBestHash(v *modrinthApi.File) (string, string) {
	// 优先尝试首选哈希；SHA1是Modrinth包导出所必需的，但
	// SHA512也是，所以我们无法用当前的单哈希格式获胜
	val, exists := v.Hashes["sha512"]
	if exists {
		return "sha512", val
	}
	val, exists = v.Hashes["sha256"]
	if exists {
		return "sha256", val
	}
	val, exists = v.Hashes["sha1"]
	if exists {
		return "sha1", val
	}
	val, exists = v.Hashes["murmur2"] // （在Modrinth包规范中未定义，请谨慎使用）
	if exists {
		return "murmur2", val
	}

	// 没有首选哈希存在，只需获取第一个
	for key, val := range v.Hashes {
		return key, val
	}

	// 没有哈希存在
	return "", ""
}

func getInstalledProjectIDs(index *core.Index) []string {
	var installedProjects []string
	// 获取所有模组的modids
	mods, err := index.LoadAllMods()
	if err != nil {
		fmt.Printf("无法确定现有项目：%v\n", err)
	} else {
		for _, mod := range mods {
			data, ok := mod.GetParsedUpdateData("modrinth")
			if ok {
				updateData, ok := data.(mrUpdateData)
				if ok {
					if len(updateData.ProjectID) > 0 {
						installedProjects = append(installedProjects, updateData.ProjectID)
					}
				}
			}
		}
	}
	return installedProjects
}

func resolveVersion(project *modrinthApi.Project, version string) (*modrinthApi.Version, error) {
	// 如果它存在于版本列表中，它已经是一个版本ID（并且不需要进一步查询）
	if slices.Contains(project.Versions, version) {
		versionData, err := mrDefaultClient.Versions.Get(version)
		if err != nil {
			return nil, fmt.Errorf("获取版本 %s 失败：%v", version, err)
		}
		return versionData, nil
	}

	// 查找所有版本
	// TODO: 向Modrinth提交PR以添加版本号过滤器？
	versionsList, err := mrDefaultClient.Versions.ListVersions(*project.ID, modrinthApi.ListVersionsOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取 %s 的版本列表失败：%v", *project.ID, err)
	}
	// 反向遍历：Modrinth knossos总是给最旧的文件而不是版本号路径优先权
	for i := len(versionsList) - 1; i >= 0; i-- {
		if *versionsList[i].VersionNumber == version {
			return versionsList[i], nil
		}
	}
	return nil, fmt.Errorf("无法找到版本 %s", version)
}

// mapDepOverride 转换手动依赖覆盖（当packwiz能够确定提供的mod时，这可能会被移除）
func mapDepOverride(depID string, isQuilt bool, mcVersion string) string {
	if isQuilt && (depID == "P7dR8mSH" || depID == "fabric-api") {
		// Transform FAPI dependencies to QFAPI/QSL dependencies when using Quilt
		return "qvIfYCYJ"
	}
	if isQuilt && (depID == "Ha28R6CL" || depID == "fabric-language-kotlin") {
		// Transform FLK dependencies to QKL dependencies when using Quilt >=1.19.2 non-snapshot
		if flexver.Less("1.19.1", mcVersion) && flexver.Less(mcVersion, "2.0.0") {
			return "lwVhp9o5"
		}
	}
	return depID
}
