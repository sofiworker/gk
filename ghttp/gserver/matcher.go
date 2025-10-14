package gserver

//type MatchResult struct {
//	//Handler    fasthttp.RequestHandler
//	//Middleware []Middleware
//	Params    map[string]string
//	RoutePath string
//}
//
//type MatcherStats struct {
//	TotalRequests    uint64
//	MatchHits        uint64
//	MatchMisses      uint64
//	AvgMatchTimeNs   uint64
//	MemoryUsageBytes uint64
//	RoutesCount      int
//}
//
//// Matcher 核心接口 - 负责路由匹配
//type Matcher interface {
//	Match(method, path string) *MatchResult
//	AddRoute(method, path string, handler ...Handler) error
//	RemoveRoute(method, path string) error
//	Stats() *MatcherStats
//}
