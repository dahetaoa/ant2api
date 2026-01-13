package common

// AccountContext 表示一次请求转发到后端所需的账号上下文信息。
// 该结构作为网关层（providers）共享类型，避免在多个 convert.go 中重复定义。
type AccountContext struct {
	ProjectID   string
	SessionID   string
	AccessToken string
}
