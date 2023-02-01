package main

import (
	"context"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Checker struct {
	Name      string   `yaml:"name"`
	Targets   []Target `yaml:"targets"`
	Proxy     string   `yaml:"proxy"`
	Threshold int      `yaml:"threshold"`
	PostUp    string   `yaml:"postUp"`
	PostDown  string   `yaml:"postDown"`
	//----------
	status    int
	failCount int
}

type Target struct {
	IP   string `yaml:"ip"`
	Host string `yaml:"host"`
}

func main() {
	file := flag.String("c", "config.yaml", "config location")
	flag.Parse()
	log.Println("Network Guard")
	open, err := os.Open(*file)
	if err != nil {
		log.Fatalf("read config error: %s", err)
	}
	checkers := new([]Checker)
	err = yaml.NewDecoder(open).Decode(checkers)
	if err != nil {
		log.Fatalf("parse config error: %s", err)
	}
	ctx := context.Background()
	for _, c := range *checkers {
		log.Printf("%+v", c)
		go c.Check(ctx)
	}
	select {}
}

const (
	UP = iota
	DOWN
)

func (c *Checker) Check(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
		default:
			switch c.status {
			case UP:
				ret := c.HttpCheck(ctx, 2000*time.Millisecond)
				if ret.Status {
					if c.failCount > 1 {
						log.Printf("%s recover", c.Name)
					}
					c.failCount = 0
				} else {
					c.failCount++
					if c.failCount >= c.Threshold {
						log.Printf("%s from UP to DOWN", c.Name)
						c.status = DOWN
						c.failCount = c.Threshold
						if c.PostDown != "" {
							RunExternalCmd(c.PostDown)
							log.Printf("%s PostDown executed", c.Name)
						}
						time.Sleep(5 * time.Second)
					} else {
						log.Printf("%s jitter", c.Name)
						time.Sleep(3 * time.Second)
					}
				}
			case DOWN:
				ret := c.HttpCheck(ctx, 1000*time.Millisecond)
				if ret.Status {
					c.failCount -= 1
					if c.failCount <= 0 {
						log.Printf("%s from DOWN to UP", c.Name)
						c.status = UP
						c.failCount = 0
						if c.PostUp != "" {
							RunExternalCmd(c.PostDown)
							log.Printf("%s PostUp executed", c.Name)
						}
					}
					time.Sleep(2 * time.Second)
				} else {
					c.failCount = c.Threshold
					time.Sleep(3 * time.Second)
				}
			}
		}
	}

}

func (c *Checker) HttpCheck(pctx context.Context, timeout time.Duration) CheckResult {
	result := make(chan interface{})
	ctx, cancelFunc := context.WithCancel(pctx)
	defer cancelFunc()
	for _, target := range c.Targets {
		go func(t Target) {
			status := HttpCheck(t, c.Proxy, timeout)
			select {
			case <-ctx.Done():
				return
			case result <- status:
				return
			}
		}(target)
	}
	for range c.Targets {
		ret := <-result
		if r, ok := ret.(CheckResult); ok {
			if r.Status {
				return r
			}
		}
	}
	return CheckResult{Status: false}
}

type CheckResult struct {
	Status  bool
	Message string
}

var StatusOk = CheckResult{true, ""}

func HttpCheck(target Target, proxyStr string, timeout time.Duration) CheckResult {
	var proxy = http.ProxyFromEnvironment
	if strings.TrimSpace(proxyStr) != "" {
		proxy = http.ProxyURL(nil)
	}
	httpClient := http.Client{
		Transport: &http.Transport{Proxy: proxy},
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fmt.Sprintf("http://%s", target.IP), nil)
	if err != nil {
		return CheckResult{false, fmt.Sprintf("create request fail: %s", err.Error())}
	}
	req.Header.Set("Host", target.Host)
	if _, err := httpClient.Do(req); err != nil {
		return CheckResult{false, fmt.Sprintf("send request fail: %s", err.Error())}
	}
	return StatusOk
}

func TcpPing(addr string) error {
	tcpAddr, err := net.ResolveTCPAddr("", addr)
	if err != nil {
		return err
	}
	dialer, err := net.DialTCP("tcp", nil, tcpAddr)
	_, err = dialer.Write([]byte{})
	return err
}

func RunExternalCmd(cmd string) {
	split := strings.Split(cmd, " ")
	output, err := exec.Command(split[0], split[1:]...).Output()
	if err != nil {
		log.Println(err)
	}
	log.Println(string(output))
}
