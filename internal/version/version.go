package version

// InfoValue describes build metadata that can be shared by the CLI, server, and
// diagnostics endpoints. Keeping this structure in a dedicated package avoids
// hardcoding project identity in multiple command entrypoints.
//
// InfoValue 描述了可供 CLI 工具、服务端以及诊断端点共享的构建元数据。
// 将该结构体单独放置于一个专有的包中，可有效避免在多个命令入口点处
// 硬编码项目的身份信息。
type InfoValue struct {
	// Name is the stable project identifier used in logs, CLI output, and reports.
	Name string
	// Version is the semantic or development version displayed to operators.
	Version string
}

const developmentVersion = "dev"

// Info returns the current project metadata for user-facing commands and service
// diagnostics. The function currently reports a development version because the
// repository scaffold does not yet have release-time linker injection.
//
// Info 为面向用户的命令行工具以及服务诊断端点返回当前项目的元数据。
// 由于目前的仓库脚手架尚未引入发布时 (release-time) 的链接器注入机制，
// 该函数当前始终报告一个开发版 ("dev") 的版本号。
func Info() InfoValue {
	return InfoValue{Name: "vdb-guardian", Version: developmentVersion}
}
