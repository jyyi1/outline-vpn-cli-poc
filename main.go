package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"

	"github.com/songgao/water"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const OUTLINE_TUN_NAME = "outline233"
const OUTLINE_TUN_IP = "10.233.233.1"
const OUTLINE_TUN_SUBNET = "10.233.233.1/32"
const OUTLINE_GW_SUBNET = "10.233.233.2/32"
const OUTLINE_GW_IP = "10.233.233.2"
const OUTLINE_ROUTING_PRIORITY = 23333
const OUTLINE_ROUTING_TABLE = 233

// ./app
//
//	<svr-ip>     : the outline server IP (e.g. 111.111.111.111/32)
//	<svr-pass>   : the outline server password
func main() {
	fmt.Println("OutlineVPN CLI (experimental-01271722)")

	svrIp := os.Args[1]

	tun, err := setupTunDevice()
	if err != nil {
		return
	}
	defer cleanUpTunDevice(tun)

	if err := showTunDevice(); err != nil {
		return
	}
	if err := configureTunDevice(); err != nil {
		return
	}
	if err := showTunDevice(); err != nil {
		return
	}

	err = setupRouting()
	if err != nil {
		return
	}
	defer cleanUpRouting()

	if err := showRouting(); err != nil {
		return
	}

	r, err := setupIpRule(svrIp)
	if err != nil {
		return
	}
	defer cleanUpRule(r)

	if err := showAllRules(); err != nil {
		return
	}

	if err := startTun2Socks(); err != nil {
		return
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, unix.SIGTERM, unix.SIGHUP)
	s := <-sigc
	fmt.Printf("\nReceived %v, cleaning up resources...\n", s)
}

func showTunDevice() error {
	l, err := netlink.LinkByName(OUTLINE_TUN_NAME)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	if tun, ok := l.(*netlink.Tuntap); ok {
		mode := "unknown"
		if tun.Mode == netlink.TUNTAP_MODE_TUN {
			mode = "tun"
		} else if tun.Mode == netlink.TUNTAP_MODE_TAP {
			mode = "tap"
		}
		persist := "persist"
		if tun.NonPersist {
			persist = "non-persist"
		}
		fmt.Printf("\t%v %v %v mtu=%v attr=%v stat=%v\n", tun.Name, mode, persist, tun.MTU, tun.Attrs(), tun.Statistics)
		return nil
	} else {
		fmt.Printf("fatal error: %v is not a tun device\n", OUTLINE_TUN_NAME)
		return fmt.Errorf("tun device not found")
	}
}

func setupTunDevice() (*water.Interface, error) {
	fmt.Println("setting up tun device...")
	conf := water.Config{
		DeviceType: water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{
			Name:    OUTLINE_TUN_NAME,
			Persist: false,
		},
	}
	r, err := water.New(conf)
	if err == nil {
		fmt.Println("tun device created")
	} else {
		fmt.Printf("fatal error: %v\n", err)
	}
	return r, err
}

func configureTunDevice() error {
	fmt.Println("configuring tun device ip...")
	tun, err := netlink.LinkByName(OUTLINE_TUN_NAME)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	addr, err := netlink.ParseAddr(OUTLINE_TUN_SUBNET)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	if err := netlink.AddrAdd(tun, addr); err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	if err := netlink.LinkSetUp(tun); err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	return nil
}

func cleanUpTunDevice(tun *water.Interface) error {
	fmt.Println("cleaning up tun device...")
	err := tun.Close()
	if err == nil {
		fmt.Println("tun device deleted")
	} else {
		fmt.Printf("clean up error: %v\n", err)
	}
	return err
}

func showRouting() error {
	filter := netlink.Route{Table: OUTLINE_ROUTING_TABLE}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &filter, netlink.RT_FILTER_TABLE)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	fmt.Printf("\tRoutes (@%v): %v\n", OUTLINE_ROUTING_TABLE, len(routes))
	for _, route := range routes {
		fmt.Printf("\t\t%v\n", route)
	}
	return nil
}

func setupRouting() error {
	fmt.Println("configuring outline routing table...")
	tun, err := netlink.LinkByName(OUTLINE_TUN_NAME)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}

	dst, err := netlink.ParseIPNet(OUTLINE_GW_SUBNET)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	r := netlink.Route{
		LinkIndex: tun.Attrs().Index,
		Table:     OUTLINE_ROUTING_TABLE,
		Dst:       dst,
		Src:       net.ParseIP(OUTLINE_TUN_IP),
		Scope:     netlink.SCOPE_LINK,
	}
	fmt.Printf("\trouting only from %v to %v through nic %v...\n", r.Src, r.Dst, r.LinkIndex)
	err = netlink.RouteAdd(&r)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}

	r = netlink.Route{
		LinkIndex: tun.Attrs().Index,
		Table:     OUTLINE_ROUTING_TABLE,
		Gw:        net.ParseIP(OUTLINE_GW_IP),
	}
	fmt.Printf("\tdefault routing entry via gw %v through nic %v...\n", r.Gw, r.LinkIndex)
	err = netlink.RouteAdd(&r)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}

	fmt.Println("routing table has been successfully configured")
	return nil
}

func cleanUpRouting() error {
	fmt.Println("cleaning up outline routing table...")
	filter := netlink.Route{Table: OUTLINE_ROUTING_TABLE}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &filter, netlink.RT_FILTER_TABLE)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	var lastErr error = nil
	for _, route := range routes {
		if err := netlink.RouteDel(&route); err != nil {
			fmt.Printf("fatal error: %v\n", err)
			lastErr = err
		}
	}
	if lastErr == nil {
		fmt.Println("routing table has been reset")
	}
	return lastErr
}

func showAllRules() error {
	rules, err := netlink.RuleList(netlink.FAMILY_ALL)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	for _, r := range rules {
		fmt.Printf("\t%v\n", r)
	}
	return nil
}

func setupIpRule(svrIp string) (*netlink.Rule, error) {
	fmt.Println("adding ip rule for outline routing table...")
	dst, err := netlink.ParseIPNet(svrIp)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return nil, err
	}
	rule := netlink.NewRule()
	rule.Priority = OUTLINE_ROUTING_PRIORITY
	rule.Family = netlink.FAMILY_V4
	rule.Table = OUTLINE_ROUTING_TABLE
	rule.Dst = dst
	rule.Invert = true
	fmt.Printf("+from all not to %v via table %v...\n", rule.Dst, rule.Table)
	err = netlink.RuleAdd(rule)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return nil, err
	}
	fmt.Println("ip rule for outline routing table created")
	return rule, nil
}

func cleanUpRule(rule *netlink.Rule) error {
	fmt.Println("cleaning up ip rule of routing table...")
	err := netlink.RuleDel(rule)
	if err != nil {
		fmt.Printf("fatal error: %v\n", err)
		return err
	}
	fmt.Println("ip rule of routing table deleted")
	return nil
}

func startTun2Socks() error {
	return fmt.Errorf("not implemented yet")
}
