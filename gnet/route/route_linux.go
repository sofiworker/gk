//go:build linux

package route

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

func listRoutes(family int) ([]Route, error) {
	routes, err := netlink.RouteListFiltered(familyOrAll(family), &netlink.Route{}, 0)
	if err != nil {
		return nil, fmt.Errorf("route list: %w", err)
	}
	return convertRoutes(routes), nil
}

func addRoute(r Route) error {
	nlr := toNetlinkRoute(r)
	if err := netlink.RouteAdd(&nlr); err != nil {
		return fmt.Errorf("route add: %w", err)
	}
	return nil
}

func deleteRoute(r Route) error {
	nlr := toNetlinkRoute(r)
	if err := netlink.RouteDel(&nlr); err != nil {
		return fmt.Errorf("route del: %w", err)
	}
	return nil
}

func familyOrAll(fam int) int {
	if fam == 0 {
		return netlink.FAMILY_ALL
	}
	return fam
}

func convertRoutes(routes []netlink.Route) []Route {
	out := make([]Route, 0, len(routes))
	for _, r := range routes {
		out = append(out, Route{
			Dst:      r.Dst,
			Src:      r.Src,
			Gw:       r.Gw,
			IfIndex:  r.LinkIndex,
			Table:    r.Table,
			Priority: r.Priority,
			Protocol: int(r.Protocol),
			Scope:    int(r.Scope),
			Type:     r.Type,
		})
	}
	return out
}

func toNetlinkRoute(r Route) netlink.Route {
	return netlink.Route{
		Dst:       r.Dst,
		Src:       r.Src,
		Gw:        r.Gw,
		LinkIndex: r.IfIndex,
		Table:     r.Table,
		Priority:  r.Priority,
		Protocol:  netlink.RouteProtocol(r.Protocol),
		Scope:     netlink.Scope(r.Scope),
		Type:      r.Type,
	}
}
