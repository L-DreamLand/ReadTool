package kb

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutside 表示请求的路径跑出了知识库根目录。
var ErrOutside = errors.New("路径越界")

// ErrNotFound 表示路径在知识库内不存在。
var ErrNotFound = errors.New("文件不存在")

// Resolve 把 KB 相对路径解析成绝对路径。
// 这是唯一允许产出"可以交给 os.Open 的路径"的地方（设计规格 §1 红线 1）。
func Resolve(root, rel string) (abs string, norm string, err error) {
	if root == "" {
		return "", "", ErrOutside
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}

	// 统一分隔符后再判断，避免 Windows 上用 "/" 拼出 "..\.." 绕过检查
	r := strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	r = strings.TrimPrefix(r, "/")
	if r == "" {
		return "", "", ErrNotFound
	}
	// 拒绝任何 ".." 段：知识库内不存在需要上跳的合法条目
	for _, seg := range strings.Split(r, "/") {
		if seg == ".." {
			return "", "", ErrOutside
		}
	}
	if filepath.IsAbs(r) || (len(r) > 1 && r[1] == ':') {
		return "", "", ErrOutside
	}

	norm = filepath.ToSlash(filepath.Clean(r))
	abs = filepath.Join(rootAbs, filepath.FromSlash(norm))

	// 二次确认：Clean 之后仍必须在根目录之内（防符号链接以外的意外拼接）
	relCheck, err := filepath.Rel(rootAbs, abs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", "", ErrOutside
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if info.IsDir() {
		return "", "", ErrNotFound
	}
	return abs, norm, nil
}
