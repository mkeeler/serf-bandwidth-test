package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-sockaddr/template"
	"github.com/hashicorp/serf/serf"
)

/*
 client - binds to one thing and joins one thing
 server - binds multiple and joins one
*/

type Config struct {
	Name string
	Join []string
	Bind []string
	Tags map[string]string
	Help bool
}

type AppendSliceValue []string

func (s *AppendSliceValue) String() string {
	return strings.Join(*s, ",")
}

func (s *AppendSliceValue) Set(value string) error {
	if *s == nil {
		*s = make([]string, 0, 1)
	}
	*s = append(*s, value)
	return nil
}

type MapAddKVPairValue map[string]string

func (s *MapAddKVPairValue) String() string {
	return fmt.Sprintf("%v", *s)
}

func (s *MapAddKVPairValue) Set(value string) error {
	parts := strings.SplitN(value, "=", 1)

	if len(parts) != 2 {
		return fmt.Errorf("Argument not of the form: <key>=<value>")
	}

	if *s == nil {
		*s = make(map[string]string)
	}

	(*s)[parts[0]] = parts[1]
	return nil
}

func errExit(exitCode int, msgFmt string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msgFmt, args...)
	fmt.Fprintln(os.Stderr, "")
	flag.Usage()
	os.Exit(exitCode)
}

func getLoggerWithKVPairs(name string, kvpairs []interface{}) *log.Logger {
	return hclog.New(&hclog.LoggerOptions{
		Name:  fmt.Sprintf("sbt: %s", name),
		Level: hclog.Debug,
		Output: hclog.NewLeveledWriter(os.Stdout, map[hclog.Level]io.Writer{
			hclog.Warn:  os.Stderr,
			hclog.Error: os.Stderr,
		}),
	}).With(kvpairs...).StandardLogger(&hclog.StandardLoggerOptions{InferLevels: true})
}

func getLogger(name string, bindAddr string, bindPort string, tags map[string]string) *log.Logger {
	kvpairs := []interface{}{
		"addr", bindAddr, "port", bindPort,
	}

	for k, v := range tags {
		kvpairs = append(kvpairs, k, v)
	}

	return getLoggerWithKVPairs(name, kvpairs)
}

func startSerf(name string, bindAddr string, bindPort int, tags map[string]string) (*serf.Serf, error) {
	conf := serf.DefaultConfig()
	conf.Init()

	logger := getLogger(name, bindAddr, strconv.Itoa(bindPort), tags)

	if name != "" {
		conf.NodeName = name
		conf.MemberlistConfig.Name = name
	}
	conf.Tags = tags
	conf.ProtocolVersion = serf.ProtocolVersionMax
	conf.MemberlistConfig.Logger = logger
	conf.MemberlistConfig.BindAddr = bindAddr
	conf.MemberlistConfig.BindPort = bindPort
	conf.MemberlistConfig.AdvertiseAddr = bindAddr
	conf.MemberlistConfig.AdvertisePort = bindPort
	conf.Logger = logger

	return serf.Create(conf)
}

func shutdownSerfs(serfs []*serf.Serf) {
	for _, s := range serfs {
		s.Leave()
		s.Shutdown()
	}
}

type BindAddr struct {
	Original string
	Address  string
	Port     int
}

func main() {
	var conf Config

	flag.BoolVar(&conf.Help, "help", false, "Display usage")
	flag.StringVar(&conf.Name, "name", "", "Name of the node")
	flag.Var((*AppendSliceValue)(&conf.Join), "join", "Address to join the Serf instance to. Can be specified multiple times but not more than the number of times -bind is")
	flag.Var((*AppendSliceValue)(&conf.Bind), "bind", "Address to bind a serf instance to. Can be specified multiple times.")
	flag.Var((*MapAddKVPairValue)(&conf.Tags), "tag", "Tags to add to the serf instances. Can be specified multiple times.")

	flag.Parse()

	if conf.Help {
		flag.Usage()
		os.Exit(0)
	}

	if conf.Name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			errExit(1, "Failed to get the hostname: %v - please specify a value for -name", err)
		}
		conf.Name = hostname
	}

	if len(conf.Bind) < 1 {
		errExit(2, "-join parameter must be specified at least once")
	}

	if len(conf.Bind) < 1 {
		errExit(2, "-bind parameter must be specified at least once")
	}

	var joins []string
	for _, addr := range conf.Join {
		if addr == "" {
			errExit(1, "-join parameter value must not be empty")
		}

		x, err := template.Parse(addr)
		if err != nil {
			errExit(1, "-join parameter %s is invalid: %v", addr, err)
		}

		joins = append(joins, x)
	}

	var binds []BindAddr
	for _, bindAddr := range conf.Bind {
		x, err := template.Parse(bindAddr)
		if err != nil {
			errExit(3, "Failed to parse -bind parameter %s: %v", bindAddr, err)
		}

		addr, portStr, err := net.SplitHostPort(x)
		if err != nil {
			errExit(3, "Failed to parse -bind parameter %s: %v", x, err)
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			errExit(3, "Failed to parse -bind parameter %s: %v", x, err)
		}

		binds = append(binds, BindAddr{Address: addr, Port: port, Original: x})
	}

	logger := getLoggerWithKVPairs(conf.Name, nil)

	if len(joins) > len(binds) {
		logger.Printf("[WARN] More join addresses specified than bind address: %v will be ignored", joins[len(binds):])
		joins = joins[:len(binds)]
	}

	var serfs []*serf.Serf
	for _, addr := range binds {
		s, err := startSerf(conf.Name, addr.Address, addr.Port, conf.Tags)
		if err != nil {
			logger.Printf("[ERROR] Failed to create Serf for addr %s: %v", addr.Original, err)
			shutdownSerfs(serfs)
			os.Exit(4)
		}

		serfs = append(serfs, s)
	}

	stopSigCh := make(chan os.Signal, 10)
	dumpInfoCh := make(chan os.Signal, 10)
	signal.Notify(stopSigCh, os.Interrupt, syscall.SIGTERM)
	signal.Notify(dumpInfoCh, syscall.SIGUSR1)

	outstandingJoins := make(map[int]string)

	for idx, addr := range joins {
		outstandingJoins[idx] = addr
	}

	for {
		var success []int
		for idx, addr := range outstandingJoins {
			_, err := serfs[idx].Join([]string{addr}, true)

			if err == nil {
				success = append(success, idx)
			} else {
				logger.Printf("[WARN] Failed to join %s", addr)
			}
		}

		for _, idx := range success {
			delete(outstandingJoins, idx)
		}

		if len(outstandingJoins) == 0 {
			break
		}

		select {
		case <-stopSigCh:
			shutdownSerfs(serfs)
			os.Exit(0)
		case <-time.After(5 * time.Second):
			// do nothing but retry
		}
	}

	logger.Printf("[INFO] Successfully joined all addresses")

	for {
		select {
		case <-stopSigCh:
			break
		case <-dumpInfoCh:
			for idx, serf := range serfs {
				fmt.Printf("Serf (%s) - Num Nodes: %d - %+v\n", binds[idx], serf.NumNodes(), serf.Members())
			}
		}
	}
	shutdownSerfs(serfs)
	os.Exit(0)
}
