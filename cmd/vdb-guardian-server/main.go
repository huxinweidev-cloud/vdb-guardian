package main

import (
	"fmt"

	"github.com/h3xwave/vdb-guardian/internal/version"
)

// main is the future API server entrypoint. The first scaffold intentionally
// avoids starting a network listener until API routes, configuration loading,
// and graceful shutdown behavior are designed and tested.
//
// main 是未来 API 服务端的入口点。最初的脚手架刻意避免了启动网络监听器，
// 直到 API 路由、配置加载以及优雅停机 (graceful shutdown) 等行为被充分
// 设计和测试完毕。
func main() {
	info := version.Info()
	fmt.Printf("%s server scaffold %s\n", info.Name, info.Version)
}
