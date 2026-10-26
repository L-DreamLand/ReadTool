// Package readtool 是模块根包，唯一职责是把前端静态资源嵌进二进制。
//
// 为什么放在仓库根而不是 internal/webassets：
// go:embed 只能引用**当前文件所在目录及其子目录**，不能向上引用 ../web。
// 而前端必须放在仓库根的 web/ 下（零构建，直接手改 CSS/JS，见 ADR-015），
// 所以 embed 文件只能位于根目录。它不参与任何业务逻辑，只导出 WebFS。
package readtool

import "embed"

// WebFS 是 web/ 目录的原样快照（含 index.html 与 src/、styles/）。
// all: 前缀确保以 _ 或 . 开头的文件也被包含。
//
//go:embed all:web
var WebFS embed.FS
