package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	debugLevelCst = 11

	programNameCst = "goRoutes"

	dockerBridgeNameCst = "br-siden"

	signalChannelSize = 10

	promListenCst           = ":9901"
	promPathCst             = "/metrics"
	promMaxRequestsInFlight = 10
	promEnableOpenMetrics   = true

	quantileError    = 0.05
	summaryVecMaxAge = 5 * time.Minute

	goMaxProcsCst = 1
)

// GRE support
// https://github.com/vishvananda/netlink/pull/263/files

// type Gretun struct {
// https://github.com/vishvananda/netlink/blob/main/link.go#L1213

var (
	// Passed by "go build -ldflags" for the show version
	commit string
	date   string

	debugLevel int

	fullPaths map[string]string

	pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "counters",
			Name:      "goRoutes",
			Help:      "goRoutes counters",
		},
		[]string{"function", "variable", "type"},
	)
	pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "histrograms",
			Name:      "goRoutes",
			Help:      "goRoutes historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
	)
)

func main() {

	log.Println(programNameCst)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	go initSignalHandler(cancel)

	version := flag.Bool("version", false, "version")

	bridgeName := flag.String("bridgeName", dockerBridgeNameCst, "docker bridge name")

	// https://pkg.go.dev/net#Listen
	promListen := flag.String("promListen", promListenCst, "Prometheus http listening socket")
	promPath := flag.String("promPath", promPathCst, "Prometheus http path. Default = /metrics")
	// curl -s http://[::1]:9111/metrics 2>&1 | grep -v "#"
	// curl -s http://127.0.0.1:9111/metrics 2>&1 | grep -v "#"

	dl := flag.Int("dl", debugLevelCst, "nasty debugLevel")

	max := flag.Int("max", goMaxProcsCst, "GOMAXPROCS")

	flag.Parse()

	runtime.GOMAXPROCS(*max)

	if *version {
		fmt.Println("commit:", commit, "\tdate(UTC):", date)
		os.Exit(0)
	}

	debugLevel = *dl

	go initPromHandler(*promPath, *promListen)

	if debugLevel > 10 {
		log.Println("service init complete")
	}

	ethLink, errL := netlink.LinkByName(*bridgeName)
	if errL != nil {
		log.Fatal("netlink.LinkByName(*bridgeName) errL:", errL)
	}

	// https://pkg.go.dev/github.com/vishvananda/netlink
	// https://github.com/vishvananda/netlink/issues/130
	// https://www.man7.org/linux/man-pages/man7/rtnetlink.7.html

	dst := &net.IPNet{
		IP:   net.IPv4(232, 0, 0, 0),
		Mask: net.CIDRMask(8, 32),
	}

	log.Printf("dst:%v", dst)

	// https://pkg.go.dev/golang.org/x/sys/unix#pkg-constants
	// RTN_ANYCAST        = 0x4
	// RTN_MULTICAST      = 0x5
	// RTN_BLACKHOLE      = 0x6

	// https://pkg.go.dev/github.com/vishvananda/netlink#Route
	// https://www.man7.org/linux/man-pages/man7/rtnetlink.7.html
	route := netlink.Route{
		LinkIndex: ethLink.Attrs().Index,
		Dst:       dst,
		Type:      unix.RTN_MULTICAST,
		//Protocol:  unix.RTPROT_STATIC,
		//Table:     unix.RT_TABLE_DEFAULT,
	}
	log.Printf("route:%v", route)

	errR := netlink.RouteAdd(&route)
	if errR != nil {
		log.Fatal("netlink.RouteAdd(&route) errR:", errR)
	}

	log.Println(programNameCst + ": That's all Folks!")
}

// initSignalHandler sets up signal handling for the process, and
// will call cancel() when recieved
func initSignalHandler(cancel context.CancelFunc) {
	c := make(chan os.Signal, signalChannelSize)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Printf("Signal caught, closing application")
	cancel()
	os.Exit(0)
}

// initPromHandler starts the prom handler with error checking
func initPromHandler(promPath string, promListen string) {
	// https: //pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp?tab=doc#HandlerOpts
	http.Handle(promPath, promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		},
	))
	go func() {
		err := http.ListenAndServe(promListen, nil)
		if err != nil {
			log.Fatal("prometheus error", err)
		}
	}()
}

// links, err := netlink.LinkList()
// if err != nil {
// 	panic(err)
// }
// for _, link := range links {
// 	fmt.Println(link.Attrs().Name)
// }

// _, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
// // delete default route first
// if err := t.RouteDel(&netlink.Route{LinkIndex: link.Attrs().Index, Dst: defaultNet}); err != nil {
// 	if errno, ok := err.(syscall.Errno); !ok || errno != syscall.ESRCH {
// 		return fmt.Errorf("could not update default route: %s", err)
// 	}
// }

// log.Infof("Setting default gateway to %s", endpoint.Network.Gateway.IP)
// route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: defaultNet, Gw: endpoint.Network.Gateway.IP}
// if err := t.RouteAdd(route); err != nil {
// 	detail := fmt.Sprintf("failed to add gateway route for endpoint %s: %s", endpoint.Network.Name, err)
// 	return errors.New(detail)
// }

// if err := netlink.RouteAdd(rt); err != nil {
// 	if !os.IsExist(err) {
// 		return fmt.Errorf("failed to add route '%s via %v dev %v': %v",
// 			r.Destination.String(), r.NextHop, ifName, err)
// 	}
// }

// func main() {
//     la := netlink.NewLinkAttrs()
//     la.Name = "foobar"

//     l, err := netlink.LinkByName(la.Name)
//     if err == nil {
//         log.Fatalf("Link with name %s already exists: %v", la.Name, err) // HERE
//     } else {
//         myGretun := &netlink.Gretun{LinkAttrs: la}
//         myGretun.Remote = net.ParseIP("2001:da8::1")
//         myGretun.Local = net.ParseIP("2001:da8::2")
//         err := netlink.LinkAdd(myGretun)
//         if err != nil {
//             log.Fatalf("Could not add %s: %v", la.Name, err)
//         }
//         l = myGretun
//     }
//     fmt.Printf("Information about the created link: %v", l)
// }
