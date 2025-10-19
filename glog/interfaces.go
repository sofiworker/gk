package glog

// 编译期断言，确保 zapLogger 满足拆分后的细粒度接口。
var (
	_ StructuredLogger = (*zapLogger)(nil)
	_ SugaredLogger    = (*zapLogger)(nil)
	_ FormattedLogger  = (*zapLogger)(nil)
	_ ContextLogger    = (*zapLogger)(nil)
	_ WithLogger       = (*zapLogger)(nil)
	_ SyncLogger       = (*zapLogger)(nil)
	_ LevelController  = (*zapLogger)(nil)
	_ OutputController = (*zapLogger)(nil)
	_ Rotater          = (*zapLogger)(nil)
	_ CloneableLogger  = (*zapLogger)(nil)
)

// Structured 以结构化接口暴露全局 logger。
func Structured() StructuredLogger {
	return GetLogger()
}

// Sugared 以键值对接口暴露全局 logger。
func Sugared() SugaredLogger {
	return GetLogger()
}

// Formatted 以格式化接口暴露全局 logger。
func Formatted() FormattedLogger {
	return GetLogger()
}

// Contextual 以上下文接口暴露全局 logger。
func Contextual() ContextLogger {
	return GetLogger()
}

// Withable 以派生接口暴露全局 logger。
func Withable() WithLogger {
	return GetLogger()
}

// Syncable 提供 Sync 能力接口实例。
func Syncable() SyncLogger {
	return GetLogger()
}

// Levelable 提供动态调整级别接口实例。
func Levelable() LevelController {
	return GetLogger()
}

// Outputtable 提供输出控制接口实例。
func Outputtable() OutputController {
	return GetLogger()
}

// Rotatable 提供轮转能力接口实例。
func Rotatable() Rotater {
	return GetLogger()
}

// Cloneable 提供克隆能力接口实例。
func Cloneable() CloneableLogger {
	return GetLogger()
}
