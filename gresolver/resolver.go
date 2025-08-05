package gresolver

import (
	"context"
	"net"
	"sync"
)

var (
	resolverFactory *ResolverFactory
)

type Resolver interface {
	// LookupIPAddr 查找主机名对应的IP地址
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)

	// LookupHost 查找主机名对应的规范主机名和IP地址
	LookupHost(ctx context.Context, host string) ([]string, error)

	// LookupCNAME 查找主机名对应的规范名称
	LookupCNAME(ctx context.Context, host string) (string, error)

	Scheme() string
}

type ResolverFactory struct {
	mu        sync.RWMutex
	resolvers map[string]Resolver
}

func init() {
	resolverFactory = &ResolverFactory{
		resolvers: make(map[string]Resolver),
	}
}

func (f *ResolverFactory) Register(name string, resolver Resolver) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolvers[name] = resolver
}

func IsDNSStyleDomain(host string) bool {
	// 检查是否为空
	if host == "" {
		return false
	}

	// 检查是否包含点号以及点号的位置
	dotIndex := len(host) - 1
	hasDot := false

	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			hasDot = true
			dotIndex = i
			break
		}
	}

	// 如果没有点号，则为单域名
	if !hasDot {
		return false
	}

	// 如果点号在开头或结尾，不是有效的DNS域名
	if dotIndex == 0 || dotIndex == len(host)-1 {
		return false
	}

	// 如果包含点号且位置合适，则为DNS样式域名
	return true
}

func ToDNSStyleDomain(host string) string {
	// 如果已经是DNS样式域名，直接返回
	if IsDNSStyleDomain(host) {
		return host
	}

	// 如果是空字符串，返回空
	if host == "" {
		return ""
	}
	// 为单域名添加.local后缀
	return host + ".local"
}

func ToDNSQueryFormat(host string) string {
	// 如果是空字符串，返回根域名
	if host == "" {
		return "."
	}

	// 如果已经以点结尾，直接返回
	if host[len(host)-1] == '.' {
		return host
	}

	// 将域名转换为DNS样式（如果需要）
	if !IsDNSStyleDomain(host) {
		host = ToDNSStyleDomain(host)
	}

	// 添加末尾的点
	return host + "."
}
