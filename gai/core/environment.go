package core

// EnvironmentBinding 记录环境身份与执行版本，不持有活动实例或授予权限。
// EnvironmentBinding records environment identity and execution versions without owning instances or granting access.
type EnvironmentBinding struct {
	ID            string
	Revision      uint64
	PolicyVersion uint64
	Dir           string
}
