// Package arch 用测试固化分层依赖（设计规格 §2.2 规则 R1–R6）。
//
// 思路：给每个 internal 包一个层级序号，低层序号的包可以 import 更高序号的，
// 反之即为违规。新增包只改 ranks 表一处，规则自动生效——这比手写依赖白名单
// 更不容易腐化。
package arch

import (
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// rank 越小越"底层"。未列出的包（cmd/*、根包）不参与规则。
var ranks = map[string]int{
	"internal/config":   0,
	"internal/platform": 0,
	"internal/logx":     0,

	"internal/kb":    1,
	"internal/state": 1,

	"internal/render": 2,
	"internal/search": 2,
	"internal/auth":   2,

	"internal/server":  3,
	"internal/httpapi": 4,
}

// mustNotImport 是比序号更严格的额外约束：这些包连"同层或更低层"也不许 import，
// 因为它们被设计成可单独测试的纯逻辑。
var mustNotImport = map[string]bool{
	"internal/config":   true,
	"internal/platform": true,
	"internal/logx":     false, // logx 可以用 platform（日志落盘）
	"internal/kb":       true,
}

func TestLayering(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports " "}}`, "./...")
	cmd.Dir = ".." // internal/arch → 模块根
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list 失败（需要 Go 工具链在 PATH 中）：%v", err)
	}

	var violations []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		importer := strings.TrimPrefix(parts[0], "readtool/")
		deps := strings.Fields(parts[1])

		impRank, impKnown := ranks[importer]
		for _, dep := range deps {
			depPkg := strings.TrimPrefix(dep, "readtool/")
			if depPkg == dep || depPkg == "" {
				continue // 标准库 / 第三方 / 根包
			}
			depRank, depKnown := ranks[depPkg]
			if !impKnown || !depKnown {
				continue
			}
			if depRank > impRank {
				violations = append(violations, importer+" 依赖了更高层 "+depPkg)
				continue
			}
			if mustNotImport[importer] && depRank >= 0 {
				violations = append(violations, importer+" 不应 import 任何 internal 包，却 import 了 "+depPkg)
			}
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("分层违规 %d 处（见设计规格 §2.2）：\n  %s", len(violations), strings.Join(violations, "\n  "))
	}
}
