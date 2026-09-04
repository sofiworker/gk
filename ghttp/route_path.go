package ghttp

import (
	"fmt"
	"strings"
)

// 本文件负责路径处理:注册期把本框架的模板语法翻译为 gin 形式并校验;请求期对
// 请求路径做零分配的合法性预检。
// This file handles paths: at registration time it translates this framework's
// template syntax into gin form with validation; at request time it pre-checks
// the request path with zero allocation.
//
// 模板语法 / template syntax:
//   {name}     单段参数     → gin :name   / single-segment param
//   {name...}  尾部通配      → gin *name   / trailing catch-all
// 翻译为 gin 形式后交给 route_tree.go 的 addRoute(算法与语义全对齐 gin)。
// Translated to gin form, then handed to route_tree.go's addRoute (algorithm and
// semantics fully aligned with gin).

// translateTemplate 把注册模板翻译为 gin 形式路径(:name / *name)并做注册期校验。
// 返回可直接喂给 addRoute 的路径串。
// translateTemplate translates a registration template into a gin-form path
// (:name / *name) with registration-time validation. It returns a path string
// ready for addRoute.
func translateTemplate(path string) (string, error) {
	if path == "" || path[0] != '/' {
		return "", ErrEmptyPath
	}
	if path == "/" {
		return "/", nil
	}

	var b strings.Builder
	b.Grow(len(path))

	i := 0
	for i < len(path) {
		c := path[i]
		if c != '{' {
			// gin 形式路径中 ':' 和 '*' 是保留字,模板字面量里不允许出现,避免歧义。
			// ':' and '*' are reserved in gin-form paths; disallow them in
			// template literals to avoid ambiguity.
			if c == ':' || c == '*' {
				return "", fmt.Errorf("%w: reserved character %q in literal", ErrInvalidParam, string(c))
			}
			b.WriteByte(c)
			i++
			continue
		}

		// 遇到 "{":必须紧跟在 "/" 之后(参数独占一段的起点)。
		// A "{" must immediately follow "/" (a param starts its own segment).
		if i == 0 || path[i-1] != '/' {
			return "", fmt.Errorf("%w: param must follow '/' in %q", ErrInvalidParam, path)
		}
		close := strings.IndexByte(path[i:], '}')
		if close < 0 {
			return "", fmt.Errorf("%w: unterminated '{' in %q", ErrInvalidParam, path)
		}
		inner := path[i+1 : i+close]
		next := i + close + 1

		if strings.HasSuffix(inner, "...") { // catch-all
			name := inner[:len(inner)-3]
			if err := validateParamName(name); err != nil {
				return "", err
			}
			if next != len(path) {
				return "", fmt.Errorf("%w: %q", ErrCatchAllPosition, path)
			}
			// gin catch-all 形式:*name(前导 "/" 已在 literal 中写入)。
			// gin catch-all form: *name (the leading "/" was already written).
			b.WriteByte('*')
			b.WriteString(name)
		} else { // param
			if err := validateParamName(inner); err != nil {
				return "", err
			}
			b.WriteByte(':')
			b.WriteString(inner)
		}

		// "{" 后必须紧跟 "/" 或路径结束(参数独占整段)。
		// After "}" the next byte must be "/" or end of path (param owns the segment).
		if next < len(path) && path[next] != '/' {
			return "", fmt.Errorf("%w: param must span a whole segment in %q", ErrInvalidParam, path)
		}
		i = next
	}
	return b.String(), nil
}

// joinRoutePath 把分组前缀与其下注册的子路径拼成完整注册路径,并保证【连接处恰好一个
// '/'】。它是分组前缀的唯一拼接口:路由注册(Group.register)与 OpenAPI 路径模板登记
// (noteRoute)都必须经它,否则 spec 里会出现与实际注册路径不一致(甚至非法)的键。
//
// 规范化只作用于"分隔符"这一确定无疑的部分,不替用户补全缺失的语义:
//   - 左侧去掉尾部所有 '/',故 Group("/api/") + "/v1/x" 得 /api/v1/x 而非 /api//v1/x;
//   - 右侧不以 '/' 开头时补一个,故 Group("/api") + "users" 得 /api/users 而非 /apiusers;
//   - 左侧为空(根分组 "" 或 "/")时右侧原样返回,使根分组与直接在 Server 上注册【完全
//     等价】——空路径仍由 translateTemplate 报 ErrEmptyPath,不因套了一层根分组就被静默
//     放行。
//
// 右侧为 "" 或 "/" 都表示"分组自身的根",结果是【去掉尾斜杠的前缀本身】:Group("/api")
// 无论 + "" 还是 + "/",注册的都是 /api,不是 /api/。如此选择的理由:一是让 Group(P) 的
// 根等价于直接注册 P,分组退化为纯粹的路径因式分解,不额外引入一个尾斜杠变体;二是 /api/
// 仍可经 TSR 以 301 到达 /api,规范 URL 落在更常用的无尾斜杠形式上;三是 "" 早已是本包
// 既有的分组根写法(拼接前它天然得到前缀本身),继续接受它才不破坏现有调用方。
//
// joinRoutePath joins a group prefix with a sub-path registered under it,
// guaranteeing EXACTLY ONE '/' at the junction. It is the single joining point for
// group prefixes: both route registration (Group.register) and OpenAPI path
// template recording (noteRoute) must go through it, or the spec would carry keys
// inconsistent with — or even illegal for — the routes actually registered.
//
// Normalization touches only the unambiguous part, the separator; it never invents
// semantics the user omitted:
//   - the left side loses every trailing '/', so Group("/api/") + "/v1/x" yields
//     /api/v1/x rather than /api//v1/x;
//   - a right side not starting with '/' gets one, so Group("/api") + "users"
//     yields /api/users rather than /apiusers;
//   - an empty left side (root group "" or "/") returns the right side verbatim, so
//     a root group is EXACTLY equivalent to registering on the Server directly — an
//     empty path still raises ErrEmptyPath from translateTemplate instead of being
//     silently accepted just because a root group was interposed.
//
// A right side of "" or "/" both mean "the group's own root", yielding THE PREFIX
// ITSELF with trailing slashes removed: Group("/api") registers /api — not /api/ —
// for either. Rationale: it makes a group's root equivalent to registering P
// directly, so a group is pure path factoring and introduces no extra
// trailing-slash variant; /api/ still reaches /api via a 301 TSR redirect, so the
// canonical URL is the more common slash-free form; and "" is already this package's
// established group-root spelling (bare concatenation naturally produced the prefix
// itself), so continuing to accept it keeps existing callers working.
func joinRoutePath(prefix, path string) string {
	base := strings.TrimRight(prefix, "/")
	if base == "" {
		return path
	}
	if path == "" || path == "/" {
		return base
	}
	if path[0] == '/' {
		return base + path
	}
	return base + "/" + path
}

// validateParamName 校验参数名合法(非空、无保留字符)。
// validateParamName validates a parameter name (non-empty, no reserved chars).
func validateParamName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidParam)
	}
	if strings.ContainsAny(name, "/{}:*") {
		return fmt.Errorf("%w: %q contains reserved character", ErrInvalidParam, name)
	}
	return nil
}

// validateRequestPath 校验请求路径;不改写路径(零分配)。尾斜杠不在此处理——由匹配
// 层的 TSR 机制(与 gin 一致)建议重定向。根路径 "/" 直接放行。
//
// strict=false(默认快速模式):仅拦 dot 段(防路径遍历)。先用一次 SIMD IndexByte
// 粗筛 '.';无 '.' 则不可能有 dot 段,立即放行(REST 路径常态)。空段(如 //)放行,
// 交给匹配层(与 gin 一致)。
// strict=true(严格模式):完整逐段校验,dot 段与空段任一非法即返回错误。
// validateRequestPath validates the request path without rewriting it (zero
// allocation). The trailing slash is left to the match layer's TSR (as in gin).
// The root "/" passes.
//
// strict=false (default fast mode): reject only dot segments (path-traversal
// guard). A single SIMD IndexByte pre-screens for '.'; with no '.' a dot segment
// is impossible and the path passes immediately (the REST norm). Empty segments
// (e.g. //) pass through to matching (aligned with gin).
// strict=true (strict mode): full per-segment validation, rejecting any dot or
// empty segment.
func validateRequestPath(path string, strict bool) error {
	if path == "" || path[0] != '/' {
		return fmt.Errorf("%w: %q must start with '/'", ErrInvalidRequestPath, path)
	}
	if path == "/" {
		return nil
	}

	// 控制字符与 DEL 一律 400。path 是【解码后】的路径，因此 %00/%0a 这类转义正是从这里
	// 落进内部逻辑的：
	//   - %00 → 交给 os.DirFS 得到的是 EINVAL（非 ErrNotExist），静态层会误判成 500，
	//     于是任何人循环请求即可稳定制造 5xx、刷爆错误率告警；
	//   - %0a/%0d → 原样进访问日志与 panic 日志，凭空多出整行（日志注入），可伪造
	//     "管理员登录"之类的记录。
	// 两者都在【同一个入口】拦掉，下游就不必各自设防：RFC 3986 也不允许路径控制字符
	// 以裸字节出现，合法请求永不受影响。
	// Reject C0 control characters and DEL with a 400. path is the DECODED path, so
	// escapes such as %00/%0a are exactly how control bytes reach internal logic:
	//   - %00 reaches os.DirFS as EINVAL (not ErrNotExist), which the static layer
	//     misreads as a 500, so anyone looping over such URLs manufactures stable 5xx
	//     and floods error-rate alerts;
	//   - %0a/%0d land verbatim in access logs and panic logs, forging whole extra
	//     lines (log injection) such as a fake "admin login" record.
	// Handling both at ONE entry point means no downstream layer needs its own guard;
	// RFC 3986 forbids control characters in a path as bare bytes anyway, so legal
	// requests are unaffected.
	for i := 0; i < len(path); i++ {
		if path[i] < 0x20 || path[i] == 0x7f {
			return fmt.Errorf("%w: %q contains a control character", ErrInvalidRequestPath, path)
		}
	}

	if !strict {
		// 快速模式:无 '.' 直接放行;有 '.' 才细查是否存在 dot 段。
		// Fast mode: no '.' → pass; only scan for a dot segment when '.' exists.
		if strings.IndexByte(path, '.') < 0 {
			return nil
		}
		return checkDotSegments(path)
	}

	// 严格模式:逐段校验 dot 段 + 空段(零切片、零字符串比较)。
	// Strict mode: per-segment validation of dot and empty segments (zero
	// slice/compare).
	segLen := 0
	allDots := true
	for i := 1; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if segLen == 0 {
				if i != len(path) { // 结尾单斜杠交给 TSR / trailing slash left to TSR
					return fmt.Errorf("%w: %q contains an empty segment", ErrInvalidRequestPath, path)
				}
			} else if allDots && segLen <= 2 { // "." 或 ".." / "." or ".."
				return fmt.Errorf("%w: %q contains a dot segment", ErrInvalidRequestPath, path)
			}
			segLen = 0
			allDots = true
			continue
		}
		if path[i] != '.' {
			allDots = false
		}
		segLen++
	}
	return nil
}

// checkDotSegments 仅校验 dot 段(整段为 "." 或 "..")→ 错误;不管空段。
// 关键优化:dot 段必然形如 "/." 后紧跟 "/" 或路径结束,或 "/.." 后紧跟 "/" 或结束。
// 因此只需定位每个 "/." 二元组再看其后一字节,而非逐段扫描——这样 "site.css" 这类
// 段内点号(文件扩展名)完全不触发慢逻辑。
// checkDotSegments validates only dot segments (a whole segment "." or "..") →
// error; it ignores empty segments. Key optimization: a dot segment must look
// like "/." followed by "/" or end, or "/.." followed by "/" or end. So it only
// locates each "/." pair and checks the following byte, rather than scanning per
// segment — this way an in-segment dot like "site.css" (a file extension) never
// triggers the slow logic.
func checkDotSegments(path string) error {
	for i := 0; i+1 < len(path); {
		// 定位下一个 '.'。
		// Locate the next '.'.
		j := strings.IndexByte(path[i:], '.')
		if j < 0 {
			return nil
		}
		p := i + j
		// dot 段要求 '.' 的前一字节是 '/'(段起点)。
		// A dot segment requires the byte before '.' to be '/' (segment start).
		if p == 0 || path[p-1] == '/' {
			// 段为 "." :其后是 '/' 或路径结束。
			// Segment "." : followed by '/' or end of path.
			if p+1 == len(path) || path[p+1] == '/' {
				return fmt.Errorf("%w: %q contains a dot segment", ErrInvalidRequestPath, path)
			}
			// 段为 ".." :第二个 '.' 之后是 '/' 或结束。
			// Segment ".." : after a second '.', followed by '/' or end.
			if path[p+1] == '.' && (p+2 == len(path) || path[p+2] == '/') {
				return fmt.Errorf("%w: %q contains a dot segment", ErrInvalidRequestPath, path)
			}
		}
		i = p + 1
	}
	return nil
}
