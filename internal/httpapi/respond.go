package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// 错误码总表（设计规格 §6.4）。域取 auth / kb / state / search；
// 跨域通用错误用无前缀短码。新增码必须同时更新 §6.4 与 types.d.ts 的联合类型。
const (
	CodeBadRequest       = "bad_request"
	CodeUnauthorized     = "unauthorized"
	CodeBadPassword      = "auth.bad_password"
	CodeAuthLocked       = "auth.locked"
	CodePathOutside      = "kb.path_outside"
	CodePathNotFound     = "kb.path_not_found"
	CodeStateNotFound    = "state.not_found"
	CodeStateLimit       = "state.limit_exceeded"
	CodeMethodNotAllowed = "method_not_allowed"
	CodePayloadTooLarge  = "payload_too_large"
	CodeInternal         = "internal"
	CodeSearchUnavail    = "search.unavailable"
	CodeServiceUnavail   = "service_unavailable"
	CodeRateLimited      = "rate_limited"   // POST /api/kb/rescan 节流（FR-21）
	CodeScanFailed       = "kb.scan_failed" // 重扫时磁盘读取失败
)

var errNilConfig = errors.New("httpapi: Options.Cfg 不能为 nil")

// errorBody 是统一错误体：{"error":{"code","message","detail"}}。
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// JSON 写出成功响应：直接返回资源本身，不包 {data:...} 信封。
func JSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// 序列化失败是编程错误：不要把半成品写出去
		Error(w, http.StatusInternalServerError, CodeInternal, "响应序列化失败", err.Error())
		return
	}
	NoStore(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Error 写出统一错误体。detail 可选，用于放 requestID 或底层错误摘要。
func Error(w http.ResponseWriter, status int, code, message string, detail ...string) {
	body := errorBody{Error: errorDetail{Code: code, Message: message}}
	if len(detail) > 0 {
		body.Error.Detail = detail[0]
	}
	buf, err := json.Marshal(body)
	if err != nil {
		http.Error(w, `{"error":{"code":"internal","message":"编码失败"}}`, http.StatusInternalServerError)
		return
	}
	NoStore(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

// NoStore 是所有 API 响应的默认缓存策略：状态必须实时（设计规格 §6.1）。
func NoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
